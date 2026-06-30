package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"go.etcd.io/bbolt"

	"ssearch/internal/storage"
)

func newDeadCmd() *cobra.Command {
	var (
		list  bool
		retry string
	)

	cmd := &cobra.Command{
		Use:   "dead",
		Short: "Manage permanently skipped files",
		Long: `View or retry files that have been permanently skipped after
exceeding the maximum retry count (3 attempts).

Use --list to see all dead files.
Use --retry <path> to move a file back to the pending queue for retry.`,
		RunE: func(c *cobra.Command, args []string) error {
			if err := app.Init(c); err != nil {
				return err
			}
			defer app.Close()

			switch {
			case list && retry != "":
				return fmt.Errorf("cannot use both --list and --retry")
			case list:
				return app.GetBoltDB().Bolt().View(func(tx *bbolt.Tx) error {
					entries, err := storage.ListDead(tx)
					if err != nil {
						return err
					}
					if len(entries) == 0 {
						fmt.Println("No dead files.")
						return nil
					}
					for path, de := range entries {
						fmt.Printf("%s\t%s\t%s\n",
							path,
							de.LastAttempt.Format(time.RFC3339),
							de.Error,
						)
					}
					return nil
				})
			case retry != "":
				return app.GetBoltDB().Bolt().Update(func(tx *bbolt.Tx) error {
					entries, err := storage.ListDead(tx)
					if err != nil {
						return err
					}
					de, ok := entries[retry]
					if !ok {
						return fmt.Errorf("file %q not found in dead list", retry)
					}
					// Move back to pending with reset retry count.
					pe := &storage.PendingEntry{
						Error:      de.Error,
						RetryCount: 0,
					}
					if err := storage.PutPending(tx, retry, pe); err != nil {
						return err
					}
					return storage.DeleteDead(tx, retry)
				})
			default:
				fmt.Fprintln(os.Stderr, "Use --list to view dead files, or --retry <path> to retry a file")
				return nil
			}
		},
	}

	cmd.Flags().BoolVar(&list, "list", false, "list all permanently skipped files")
	cmd.Flags().StringVar(&retry, "retry", "", "move a dead file back to pending for retry")

	return cmd
}
