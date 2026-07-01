package cli

import (
	"github.com/spf13/cobra"

	"ssearch/internal/indexer"
	"ssearch/pkg/utils"
)

func newUpdateCmd() *cobra.Command {
	var (
		root         string
		filter       string
		deep         bool
		force        bool
		forcePending bool
		maxSize      string
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Smart incremental update (daily use)",
		Long: `Performs an incremental update of the search index.

By default, it only checks file sizes for changes. New files are indexed,
deleted files are removed, and files whose size hasn't changed are skipped
(ModTime is updated without re-indexing).

Use --deep to compute content hashes for size-unchanged files.
Use --force to unconditionally re-index all existing files.
Use --force-pending to retry files that previously failed to index.`,
		RunE: func(c *cobra.Command, args []string) error {
			if err := app.Init(c); err != nil {
				return err
			}
			defer app.Close()

			updateRoot := app.ResolveRoot(root)

			maxBytes, err := utils.ParseSize(maxSize)
			if err != nil {
				return err
			}

			opts := indexer.UpdateOptions{
				Root:          updateRoot,
				MaxSize:       maxBytes,
				IncludeHidden: app.IncludeHidden,
				Filter:        filter,
				Deep:          deep,
				Force:         force,
				ForcePending:  forcePending,
				Stopwords:     app.Config.ResolveStopwords(),
				Extensions:    app.Config.ResolveExtensions(),
				DictCachePath: app.DictCachePath,
			}
			return indexer.UpdateIndex(c.Context(), app.DB.Bolt(), opts)
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "project root override")
	cmd.Flags().StringVar(&filter, "filter", "", "specific file or directory to update")
	cmd.Flags().BoolVar(&deep, "deep", false, "compute QuickHash for size-unchanged files")
	cmd.Flags().BoolVar(&force, "force", false, "unconditionally rebuild all indexed files")
	cmd.Flags().BoolVar(&forcePending, "force-pending", false, "retry all pending files")
	cmd.Flags().StringVar(&maxSize, "max-size", "50MB", "maximum file size to index")

	return cmd
}
