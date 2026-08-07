package main

import (
	"github.com/spf13/cobra"
)

// orun work — the previous name of the work-plane group, kept for one
// release as a hidden deprecated alias (orun-initiatives IN6, compatibility
// ledger). Every subcommand forwards to the same run functions the
// `orun initiatives` group registers (initiatives.go); nothing is
// duplicated here and nothing here will outlive the deprecation window.
func registerWorkCommand(root *cobra.Command) {
	workCmd := &cobra.Command{
		Use:        "work",
		Hidden:     true,
		Deprecated: "use 'orun initiatives' (this alias will be removed)",
		Short:      "The work plane (deprecated alias of 'orun initiatives')",
		Long: `Deprecated alias of 'orun initiatives' — the work plane's CLI group.

Subcommands forward to the same implementations:
  import    Map a specs/ tree to the hierarchy and apply to Orun Cloud
  list      Portfolio: key, title, status, progress, needs-you, target
  edit      Edit an item's envelope (title/description/owner/target/…)
  cancel    Retire an item (task or epic) — the append-only "delete"

Run 'orun initiatives --help' for the full group.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	workCmd.AddCommand(newWorkImportCommand())
	workCmd.AddCommand(newInitiativesListCommand())
	workCmd.AddCommand(newItemEditCommand())
	workCmd.AddCommand(newItemCancelCommand())
	root.AddCommand(workCmd)
}
