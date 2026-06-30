package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"ssearch/internal/storage"
)

func newCompactCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "compact",
		Short: "Compact the database to reclaim disk space",
		Long: `Compacts the BoltDB database file by copying all active data to a new
file and replacing the original. This reclaims space from deleted or
updated files.

The database is closed during compaction. No other ssearch commands
should be running concurrently.`,
		RunE: func(c *cobra.Command, args []string) error {
			if err := app.Init(c); err != nil {
				return err
			}

			dbPath := app.DBPath
			app.Close()

			before, after, err := storage.CompactDB(dbPath)
			if err != nil {
				return fmt.Errorf("compact: %w", err)
			}

			fmt.Printf("Compacted: %s → %s (saved %s)\n",
				formatSize(before),
				formatSize(after),
				formatSize(before-after),
			)
			return nil
		},
	}
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
