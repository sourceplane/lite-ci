package main

import (
	"github.com/spf13/cobra"
)

// orun initiatives — the IS-era name of the work-plane group, kept as a
// hidden deprecated alias (orun-work-spaces WK4: the exact inverse of
// IN-F). One release visible in history, then only its visibility ever
// goes — never its behavior (WK-6). Every subcommand is the same
// constructor the visible `orun work` group registers (initiatives.go's
// addWorkSubcommands); nothing is duplicated so the alias can never
// drift.
func registerInitiativesAliasCommand(root *cobra.Command) {
	cmd := &cobra.Command{
		Use:        "initiatives",
		Hidden:     true,
		Deprecated: "use 'orun work' (the work plane's group; this alias keeps working)",
		Short:      "The work plane (deprecated alias of 'orun work')",
		Long: `Deprecated alias of 'orun work' — the work plane's CLI group.

The initiative retired to a Space (orun-work-spaces): status, health,
updates and dates live on the Epic. Every subcommand here forwards to the
same implementations the 'orun work' group serves.

Run 'orun work --help' for the full group.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	addWorkSubcommands(cmd)
	root.AddCommand(cmd)
}
