package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.etcd.io/bbolt"

	"ssearch/internal/storage"
)

func newPendingCmd() *cobra.Command {
	var (
		list  bool
		clear bool
	)

	cmd := &cobra.Command{
		Use:   "pending",
		Short: "Manage pending files (files that failed to index)",
		Long: `View or clear the list of files that could not be indexed due to
encoding errors, lock contention, or size limits.

Use --list to see all pending files with their error messages.
Use --clear to remove all entries.
Use 'ssearch update --force-pending' to retry indexing pending files.`,
		RunE: func(c *cobra.Command, args []string) error {
			if err := app.Init(c); err != nil {
				return err
			}
			defer app.Close()

			switch {
			case list && clear:
				return fmt.Errorf("cannot use both --list and --clear")
			case list:
				return app.GetBoltDB().Bolt().View(func(tx *bbolt.Tx) error {
					entries, err := storage.ListPending(tx)
					if err != nil {
						return err
					}
					if len(entries) == 0 {
						fmt.Println("No pending files.")
						return nil
					}
					for path, pe := range entries {
						fmt.Printf("%s\t(retry:%d) %s\n", path, pe.RetryCount, pe.Error)
					}
					return nil
				})
			case clear:
				return app.GetBoltDB().Bolt().Update(func(tx *bbolt.Tx) error {
					if err := storage.ClearPendingTx(tx); err != nil {
						return err
					}
					// Recreate the bucket since ClearPendingTx deletes it.
					_, err := tx.CreateBucketIfNotExists(storage.BucketPending)
					return err
				})
			default:
				fmt.Fprintln(os.Stderr, "Use --list to view pending files, or --clear to clear all")
				return nil
			}
		},
	}

	cmd.Flags().BoolVar(&list, "list", false, "list all pending files")
	cmd.Flags().BoolVar(&clear, "clear", false, "clear all pending entries")

	return cmd
}
