package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"ssearch/internal/indexer"
	"ssearch/internal/search"
)

func newSearchCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "search [keywords...]",
		Short: "Multi-keyword AND search",
		Long: `Searches the index for files containing ALL of the given keywords.

Keywords are tokenized with Chinese word segmentation and matched against
the inverted index. Results are sorted by modification time (newest first).

Output format: ModTime<TAB>Path<TAB>Size`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := app.Init(c); err != nil {
				return err
			}
			defer app.Close()

			// Initialize tokenizer for keyword tokenization.
			tok, err := indexer.NewTokenizer(app.DictCachePath, nil)
			if err != nil {
				return fmt.Errorf("init tokenizer: %w", err)
			}

			// Tokenize each keyword in precise mode to preserve compounds like "自旋".
			var terms []string
			for _, kw := range args {
				tokens := tok.TokenizePrecise(kw)
				terms = append(terms, tokens...)
			}

			if len(terms) == 0 {
				fmt.Fprintln(os.Stderr, "No searchable terms after tokenization and stopword filtering")
				return nil
			}

			// Search using the search package.
			searcher := search.New(app.DB.Bolt())
			results, _, err := searcher.Search(terms, limit)
			if err != nil {
				return fmt.Errorf("search: %w", err)
			}

			if len(results) == 0 {
				fmt.Fprintf(os.Stderr, "No matches found for keywords: %v\n", args)
				return nil
			}

			// Print results.
			for _, r := range results {
				fmt.Printf("%s\t%s\t%d\n",
					r.ModTime.Format(time.RFC3339),
					r.Path,
					r.Size,
				)
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 100, "max results (0 = no limit)")

	return cmd
}
