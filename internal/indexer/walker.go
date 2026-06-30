package indexer

import (
	"context"
	"io/fs"
	"path/filepath"

	"ssearch/pkg/utils"
)

// FileEntry represents a file discovered during tree traversal.
type FileEntry struct {
	Path    string
	Size    int64
	ModTime int64 // Unix nano
	IsDir   bool
}

// WalkFiles walks the file tree rooted at root, sending FileEntry values
// on the returned channel. It respects the exclusion rules per PRD §5.1:
//
//   - Resolves root symlink, skips subdirectory symlinks.
//   - Skips hidden files/dirs unless includeHidden is true.
//   - Hard-coded skip: .ssearch, data.db, .lock, *.tmp, *.temp.
//   - Skips directories named .ssearch entirely.
//
// The channel is closed when the walk completes or ctx is cancelled.
func WalkFiles(ctx context.Context, root string, includeHidden bool) (<-chan FileEntry, <-chan error) {
	fileCh := make(chan FileEntry, 256)
	errCh := make(chan error, 1)

	go func() {
		defer close(fileCh)
		defer close(errCh)

		// Resolve root symlink.
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			realRoot = root
		}

		err = filepath.WalkDir(realRoot, func(path string, d fs.DirEntry, err error) error {
			// Check context cancellation.
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if err != nil {
				// Permission denied, path too long: skip silently per PRD §7.
				return nil
			}

			name := d.Name()

			// Hard-coded skips.
			if utils.ShouldSkip(name) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}

			// Skip hidden entries unless requested.
			if !includeHidden && utils.IsHidden(name) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}

			// Skip .ssearch directory entirely.
			if d.IsDir() && name == ".ssearch" {
				return fs.SkipDir
			}

			// Skip symlinks in subdirectories.
			if d.Type()&fs.ModeSymlink != 0 {
				return nil
			}

			// Process file.
			if d.Type().IsRegular() {
				info, err := d.Info()
				if err != nil {
					return nil // skip files we can't stat
				}

				entry := FileEntry{
					Path:    path,
					Size:    info.Size(),
					ModTime: info.ModTime().UnixNano(),
				}
				select {
				case fileCh <- entry:
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			return nil
		})

		if err != nil && err != context.Canceled {
			errCh <- err
		}
	}()

	return fileCh, errCh
}
