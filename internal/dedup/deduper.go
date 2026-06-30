package dedup

import (
	"context"
	"fmt"
	"os"
	"sort"

	"ssearch/internal/indexer"
)

// DuplicateGroup holds a set of files with identical MD5 hashes.
type DuplicateGroup struct {
	MD5   string
	Size  int64
	Files []string
}

// Deduper finds duplicate files by computing their full MD5 hashes.
type Deduper struct{}

// New creates a new Deduper.
func New() *Deduper {
	return &Deduper{}
}

// FindDuplicates walks the file tree, computes FullMD5 for each file,
// and prints groups of duplicates to stdout.
func (d *Deduper) FindDuplicates(ctx context.Context, root string, includeHidden bool) error {
	fmt.Fprintf(os.Stderr, "Scanning %s for duplicates...\n", root)

	fileCh, errCh := indexer.WalkFiles(ctx, root, includeHidden)

	// Group files by MD5.
	md5map := make(map[string]*DuplicateGroup)
	var totalFiles int64

	for entry := range fileCh {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		totalFiles++
		if totalFiles%100 == 0 {
			fmt.Fprintf(os.Stderr, "\rScanned %d files...", totalFiles)
		}

		md5hash, err := indexer.FullMD5(entry.Path)
		if err != nil {
			continue // skip unreadable files
		}
		if md5hash == "" {
			continue
		}

		grp, exists := md5map[md5hash]
		if !exists {
			grp = &DuplicateGroup{
				MD5:  md5hash,
				Size: entry.Size,
			}
			md5map[md5hash] = grp
		}
		grp.Files = append(grp.Files, entry.Path)
	}

	// Check walk errors.
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("walk: %w", err)
		}
	default:
	}

	fmt.Fprintf(os.Stderr, "\rScanned %d files.           \n", totalFiles)

	// Collect and print duplicate groups.
	var dups []DuplicateGroup
	for _, grp := range md5map {
		if len(grp.Files) > 1 {
			sort.Strings(grp.Files)
			dups = append(dups, *grp)
		}
	}

	if len(dups) == 0 {
		fmt.Println("No duplicate files found.")
		return nil
	}

	// Sort groups by size descending.
	sort.Slice(dups, func(i, j int) bool {
		return dups[i].Size > dups[j].Size
	})

	fmt.Printf("\nFound %d duplicate groups:\n\n", len(dups))
	for _, grp := range dups {
		fmt.Printf("MD5: %s  Size: %d bytes (%s)\n", grp.MD5, grp.Size, formatSize(grp.Size))
		for _, f := range grp.Files {
			fmt.Printf("  %s\n", f)
		}
		fmt.Println()
	}

	return nil
}

func formatSize(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1f GB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1f MB", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1f KB", float64(n)/1000)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
