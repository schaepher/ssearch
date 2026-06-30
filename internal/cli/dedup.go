package cli

import (
	"github.com/spf13/cobra"

	"ssearch/internal/dedup"
)

func newDedupCmd() *cobra.Command {
	var root string

	cmd := &cobra.Command{
		Use:   "dedup",
		Short: "Find duplicate files by MD5 hash",
		Long: `Walks the file tree and computes the MD5 hash of every supported file,
grouping files with identical hashes. Only groups with 2+ files are shown.

This operates independently of the search index and does not require
a prior 'ssearch index' run.`,
		RunE: func(c *cobra.Command, args []string) error {
			if err := app.Init(c); err != nil {
				return err
			}
			defer app.Close()

			dedupRoot := app.ResolveRoot(root)

			d := dedup.New()
			return d.FindDuplicates(c.Context(), dedupRoot, app.IncludeHidden)
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "project root override")

	return cmd
}
