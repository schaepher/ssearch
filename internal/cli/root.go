package cli

import (
	"context"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ssearch",
	Short: "Full-text search for local text files with Chinese word segmentation",
	Long: `ssearch is a cross-platform CLI tool that builds an inverted index
over local text files using Chinese word segmentation, enabling
millisecond keyword-path retrieval.

Run 'ssearch index' to build the index, then 'ssearch search <keywords>'
to find files. Use 'ssearch update' for daily incremental updates.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command with the given context.
func Execute(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}

func init() {
	// Global persistent flags.
	rootCmd.PersistentFlags().StringVar(&app.DBPath, "db", "",
		"database file path (default: <data-dir>/data.db)")
	rootCmd.PersistentFlags().BoolVar(&app.IncludeHidden, "include-hidden", false,
		"include hidden files and directories (those starting with .)")

	// Register subcommands.
	rootCmd.AddCommand(newIndexCmd())
	rootCmd.AddCommand(newUpdateCmd())
	rootCmd.AddCommand(newSearchCmd())
	rootCmd.AddCommand(newDedupCmd())
	rootCmd.AddCommand(newPendingCmd())
	rootCmd.AddCommand(newDeadCmd())
	rootCmd.AddCommand(newCompactCmd())
}
