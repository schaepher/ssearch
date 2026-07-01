package cli

import (
	"github.com/spf13/cobra"

	"ssearch/internal/indexer"
	"ssearch/pkg/utils"
)

func newIndexCmd() *cobra.Command {
	var (
		root      string
		maxSize   string
		stopwords string
	)

	cmd := &cobra.Command{
		Use:   "index",
		Short: "Full rebuild of the search index (clears old data)",
		Long: `Performs a full index rebuild by clearing all existing data and
re-processing every supported file in the project root.

Files are walked recursively, decoded to UTF-8, tokenized with Chinese
word segmentation, and stored in the inverted index.`,
		RunE: func(c *cobra.Command, args []string) error {
			if err := app.Init(c); err != nil {
				return err
			}
			defer app.Close()

			indexRoot := app.ResolveRoot(root)

			maxBytes, err := utils.ParseSize(maxSize)
			if err != nil {
				return err
			}

			// Resolve stopwords.
			sw := app.Config.ResolveStopwords()
			if stopwords != "" {
				sw, err = indexer.LoadStopwordsFile(stopwords)
				if err != nil {
					return err
				}
			}

			opts := indexer.IndexOptions{
				Root:          indexRoot,
				MaxSize:       maxBytes,
				IncludeHidden: app.IncludeHidden,
				Stopwords:     sw,
				Extensions:    app.Config.ResolveExtensions(),
				DictCachePath: app.DictCachePath,
			}
			return indexer.BuildIndex(c.Context(), app.DB.Bolt(), opts)
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "project root (overrides auto-discovery)")
	cmd.Flags().StringVar(&maxSize, "max-size", "50MB", "maximum file size to index")
	cmd.Flags().StringVar(&stopwords, "stopwords", "", "path to custom stopwords file (one word per line)")

	return cmd
}
