package indexer

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.etcd.io/bbolt"

	"ssearch/internal/storage"
	"ssearch/pkg/utils"
)

// UpdateOptions configures an incremental index update.
type UpdateOptions struct {
	Root          string
	MaxSize       int64
	IncludeHidden bool
	Filter        string // specific file or directory to update
	Deep          bool   // always check QuickHash
	Force         bool   // unconditionally rebuild all files
	ForcePending  bool   // retry all pending files
	Stopwords     map[string]bool
	Extensions    []string
	DictCachePath string
}

// UpdateIndex performs an incremental update of the index.
// Algorithm per PRD §5.2:
//
//  1. If ForcePending: retry all pending entries.
//  2. Walk current file tree. For each file:
//     a. New file → index.
//     b. Existing file, Force → re-index.
//     c. Existing file, Size unchanged, !Deep: ModTime change → TouchModTime; skip.
//     d. Existing file, Size unchanged, Deep: QuickHash → MD5 → re-index or skip.
//     e. Existing file, Size changed: QuickHash → MD5 → re-index or update size.
//  3. Detect deleted files → remove from all indices.
func UpdateIndex(ctx context.Context, bdb *bbolt.DB, opts UpdateOptions) error {
	fmt.Fprintf(os.Stderr, "Updating index for %s...\n", opts.Root)

	tokenizer, err := NewTokenizer(opts.DictCachePath, opts.Stopwords)
	if err != nil {
		return fmt.Errorf("init tokenizer: %w", err)
	}

	// Phase 0: Retry pending if requested.
	if opts.ForcePending {
		if err := retryPending(ctx, bdb, tokenizer, opts); err != nil {
			return fmt.Errorf("retry pending: %w", err)
		}
	}

	// Phase 1: Walk current file tree.
	fileCh, errCh := WalkFiles(ctx, opts.Root, opts.IncludeHidden)

	// Track which existing IDs we've seen (for deletion detection).
	var (
		seenIDs   = make(map[uint64]bool)
		totalNew  int64
		totalUpd  int64
		totalSkip int64
		totalDel  int64
		totalPend int64
		startTime = time.Now()
	)

	for entry := range fileCh {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip files with unsupported extensions.
		if !utils.IsSupportedExt(entry.Path, opts.Extensions) {
			continue
		}

		var action string

		err := bdb.Update(func(tx *bbolt.Tx) error {
			existingID, exists := storage.LookupFileID(tx, entry.Path)

			if exists {
				seenIDs[existingID] = true

				// Get stored metadata.
				fm, err := storage.GetFileMeta(tx, existingID)
				if err != nil {
					// Metadata corrupt or missing; treat as new.
					exists = false
				}

				if exists {
					// Decision per update matrix.
					if opts.Force {
						// Unconditional re-index.
						action = "reindex"
						goto doReindex
					}

					if entry.Size == fm.Size {
						if opts.Deep {
							// Size same, Deep: check QuickHash.
							qh, qhErr := QuickHash(entry.Path)
							if qhErr != nil {
								totalPend++
								return storage.PutPending(tx, entry.Path, &storage.PendingEntry{
									Error: fmt.Sprintf("quickhash: %v", qhErr),
								})
							}
							if qh == fm.QuickHash {
								// Content identical; maybe update ModTime.
								newModTime := time.Unix(0, entry.ModTime)
								if !newModTime.Equal(fm.ModTime) {
									_ = storage.TouchModTime(tx, existingID, newModTime)
									action = "modtime"
									totalUpd++
								} else {
									action = "skip"
									totalSkip++
								}
								return nil
							}
							// QuickHash differs; compute MD5.
							md5hash, _ := FullMD5(entry.Path)
							if md5hash == fm.MD5 && fm.MD5 != "" {
								fm.Size = entry.Size
								_ = storage.PutFileMeta(tx, existingID, fm)
								action = "size-update"
								totalUpd++
								return nil
							}
							// Content changed; re-index in a separate step.
							action = "reindex"
							goto doReindex
						}
						// !Deep, size same: only update ModTime if changed.
						newModTime := time.Unix(0, entry.ModTime)
						if !newModTime.Equal(fm.ModTime) {
							_ = storage.TouchModTime(tx, existingID, newModTime)
							action = "modtime"
							totalUpd++
						} else {
							action = "skip"
							totalSkip++
						}
						return nil
					}

					// Size changed.
					action = "reindex"
					goto doReindex
				}
				// If fm read failed, fall through to "new" path.
			}

			// New file: index it.
			action = "new"
		doReindex:
			// For existing files being reindexed, we need to remove old
			// inverted entries. We do reindex as: allocate new ID, index,
			// remove old ID. This avoids scanning all terms.
			if exists {
				_ = storage.RemoveFileFromInverted(tx, existingID)
				_ = storage.DeletePathMap(tx, existingID, entry.Path)
				_ = tx.Bucket(storage.BucketFileMeta).Delete(storage.Uint64ToBytes(existingID))
			}

			// Check size limit.
			if entry.Size > opts.MaxSize {
				totalPend++
				return storage.PutPending(tx, entry.Path, &storage.PendingEntry{
					Error: fmt.Sprintf("file size %d exceeds max %d", entry.Size, opts.MaxSize),
				})
			}

			// QuickHash.
			qh, err := QuickHash(entry.Path)
			if err != nil {
				totalPend++
				return storage.PutPending(tx, entry.Path, &storage.PendingEntry{
					Error: fmt.Sprintf("quickhash: %v", err),
				})
			}

			// Decode and parse.
			text, err := DetectAndDecode(entry.Path, opts.MaxSize)
			if err != nil {
				totalPend++
				return storage.PutPending(tx, entry.Path, &storage.PendingEntry{
					Error: fmt.Sprintf("decode: %v", err),
				})
			}

			// FullMD5 for dedup support.
			md5hash, _ := FullMD5(entry.Path)

			// Tokenize.
			terms := tokenizer.Tokenize(text)

			// Allocate new ID.
			newID := storage.NextFileID(tx)

			// Store.
			if err := storage.PutPathMap(tx, newID, entry.Path); err != nil {
				return err
			}
			if err := storage.PutFileMeta(tx, newID, &storage.FileMeta{
				Size:      entry.Size,
				MD5:       md5hash,
				QuickHash: qh,
				ModTime:   time.Unix(0, entry.ModTime),
			}); err != nil {
				return err
			}
			for _, term := range terms {
				if err := storage.AddTermIndex(tx, term, newID); err != nil {
					return err
				}
			}

			seenIDs[newID] = true

			if action == "reindex" {
				totalUpd++
			} else {
				totalNew++
			}
			return nil
		})

		if err != nil {
			return fmt.Errorf("update %s: %w", entry.Path, err)
		}

		// Progress indicator.
		total := totalNew + totalUpd + totalSkip
		if total%100 == 0 {
			fmt.Fprintf(os.Stderr, "\rProcessed %d files (new:%d upd:%d skip:%d)...",
				total, totalNew, totalUpd, totalSkip)
		}
	}

	// Check walk errors.
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("walk: %w", err)
		}
	default:
	}

	// Phase 2: Detect deleted files.
	if err := bdb.Update(func(tx *bbolt.Tx) error {
		allIDs, err := storage.AllPathIDs(tx)
		if err != nil {
			return err
		}
		for _, id := range allIDs {
			if !seenIDs[id] {
				path, ok := storage.LookupPath(tx, id)
				if ok {
					// Verify file truly doesn't exist (it may have been filtered).
					if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
						if err := storage.RemoveFile(tx, id, path); err != nil {
							return fmt.Errorf("remove deleted file %s: %w", path, err)
						}
						totalDel++
					}
				}
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("detect deletes: %w", err)
	}

	// Update meta root if changed.
	_ = bdb.Update(func(tx *bbolt.Tx) error {
		return storage.PutMeta(tx, "root", opts.Root)
	})

	elapsed := time.Since(startTime)
	fmt.Fprintf(os.Stderr, "\rUpdated: %d new, %d updated, %d skipped, %d deleted in %v",
		totalNew, totalUpd, totalSkip, totalDel, elapsed.Round(time.Millisecond))
	if totalPend > 0 {
		fmt.Fprintf(os.Stderr, " (%d pending)", totalPend)
	}
	fmt.Fprintln(os.Stderr)
	return nil
}

// retryPending re-processes all files in the pending bucket.
// Per PRD §7: if file no longer exists, remove from pending (don't move to dead).
// After 3 retries, move to dead.
func retryPending(ctx context.Context, bdb *bbolt.DB, tokenizer *Tokenizer, opts UpdateOptions) error {
	fmt.Fprintln(os.Stderr, "Retrying pending files...")

	var total, succeeded, removed, deadCount int

	err := bdb.Update(func(tx *bbolt.Tx) error {
		entries, err := storage.ListPending(tx)
		if err != nil {
			return err
		}

		for path, pe := range entries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			total++

			// File no longer exists → remove from pending.
			if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
				if err := storage.DeletePending(tx, path); err != nil {
					return err
				}
				removed++
				continue
			}

			pe.RetryCount++
			if pe.RetryCount >= 3 {
				// Move to dead.
				de := &storage.DeadEntry{
					Error:       pe.Error,
					LastAttempt: time.Now(),
				}
				if err := storage.MovePendingToDead(tx, path, de); err != nil {
					return err
				}
				deadCount++
				continue
			}

			// Retry indexing.
			text, err := DetectAndDecode(path, opts.MaxSize)
			if err != nil {
				pe.Error = fmt.Sprintf("decode: %v", err)
				_ = storage.PutPending(tx, path, pe)
				continue
			}

			qh, _ := QuickHash(path)
			md5hash, _ := FullMD5(path)
			terms := tokenizer.Tokenize(text)

			fi, fiErr := os.Stat(path)
			if fiErr != nil {
				continue
			}

			newID := storage.NextFileID(tx)
			if err := storage.PutPathMap(tx, newID, path); err != nil {
				return err
			}
			if err := storage.PutFileMeta(tx, newID, &storage.FileMeta{
				Size:      fi.Size(),
				MD5:       md5hash,
				QuickHash: qh,
				ModTime:   fi.ModTime(),
			}); err != nil {
				return err
			}
			for _, term := range terms {
				if err := storage.AddTermIndex(tx, term, newID); err != nil {
					return err
				}
			}

			// Success: remove from pending.
			if err := storage.DeletePending(tx, path); err != nil {
				return err
			}
			succeeded++
		}
		return nil
	})

	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Pending: %d succeeded, %d removed (deleted), %d moved to dead (of %d total)\n",
		succeeded, removed, deadCount, total)
	return nil
}
