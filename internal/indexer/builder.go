package indexer

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.etcd.io/bbolt"

	"ssearch/internal/storage"
)

// BatchSize controls how many files are indexed per BoltDB transaction.
const BatchSize = 500

// IndexOptions configures a full index rebuild.
type IndexOptions struct {
	Root          string
	MaxSize       int64
	IncludeHidden bool
	Stopwords     map[string]bool
	DictCachePath string
}

// BuildIndex performs a full index rebuild. Steps:
//  1. Clear all existing data.
//  2. Initialize tokenizer.
//  3. Set meta: version, root.
//  4. Walk files, parse, tokenize, and store in batches.
func BuildIndex(ctx context.Context, bdb *bbolt.DB, opts IndexOptions) error {
	fmt.Fprintf(os.Stderr, "Building index for %s...\n", opts.Root)

	// Phase 0: Clear and init meta.
	tokenizer, err := NewTokenizer(opts.DictCachePath, opts.Stopwords)
	if err != nil {
		return fmt.Errorf("init tokenizer: %w", err)
	}

	if err := bdb.Update(func(tx *bbolt.Tx) error {
		if err := storage.ClearAll(tx); err != nil {
			return err
		}
		if err := storage.PutMeta(tx, "root", opts.Root); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return fmt.Errorf("init db: %w", err)
	}

	// Phase 1: Walk files.
	fileCh, errCh := WalkFiles(ctx, opts.Root, opts.IncludeHidden)

	var batch []storage.IndexEntry
	var totalFiles, totalPending int64
	startTime := time.Now()

	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}
		return bdb.Update(func(tx *bbolt.Tx) error {
			return storage.IndexBatch(tx, batch)
		})
	}

	for entry := range fileCh {
		// Check for oversized files.
		if entry.Size > opts.MaxSize {
			pe := &storage.PendingEntry{
				Error:      fmt.Sprintf("file size %d exceeds max %d", entry.Size, opts.MaxSize),
				RetryCount: 0,
			}
			_ = bdb.Update(func(tx *bbolt.Tx) error {
				return storage.PutPending(tx, entry.Path, pe)
			})
			totalPending++
			continue
		}

		// Compute QuickHash.
		qh, err := QuickHash(entry.Path)
		if err != nil {
			pe := &storage.PendingEntry{
				Error:      fmt.Sprintf("quickhash: %v", err),
				RetryCount: 0,
			}
			_ = bdb.Update(func(tx *bbolt.Tx) error {
				return storage.PutPending(tx, entry.Path, pe)
			})
			totalPending++
			continue
		}

		// Detect encoding and decode to text.
		text, err := DetectAndDecode(entry.Path, opts.MaxSize)
		if err != nil {
			pe := &storage.PendingEntry{
				Error:      fmt.Sprintf("decode: %v", err),
				RetryCount: 0,
			}
			_ = bdb.Update(func(tx *bbolt.Tx) error {
				return storage.PutPending(tx, entry.Path, pe)
			})
			totalPending++
			continue
		}

		// Also compute FullMD5 for dedup support.
		md5hash, _ := FullMD5(entry.Path)

		// Tokenize.
		terms := tokenizer.Tokenize(text)
		if len(terms) == 0 {
			continue // nothing to index
		}

		// Add to batch.
		batch = append(batch, storage.IndexEntry{
			Path: entry.Path,
			FileMeta: storage.FileMeta{
				Size:      entry.Size,
				MD5:       md5hash,
				QuickHash: qh,
				ModTime:   time.Unix(0, entry.ModTime),
			},
			Terms: terms,
		})

		totalFiles++

		// Flush batch periodically.
		if len(batch) >= BatchSize {
			if err := flushBatch(); err != nil {
				return fmt.Errorf("flush batch: %w", err)
			}
			fmt.Fprintf(os.Stderr, "\rIndexed %d files...", totalFiles)
			batch = batch[:0]
		}
	}

	// Flush remaining.
	if err := flushBatch(); err != nil {
		return fmt.Errorf("flush final batch: %w", err)
	}

	// Check for walk errors.
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("walk: %w", err)
		}
	default:
	}

	elapsed := time.Since(startTime)
	fmt.Fprintf(os.Stderr, "\rIndexed %d files in %v", totalFiles, elapsed.Round(time.Millisecond))
	if totalPending > 0 {
		fmt.Fprintf(os.Stderr, " (%d pending)", totalPending)
	}
	fmt.Fprintln(os.Stderr)
	return nil
}
