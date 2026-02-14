package main

import "github.com/spf13/cobra"

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Generate execution plan from intent",
	RunE: func(cmd *cobra.Command, args []string) error {
		return generatePlan()
	},
}

func registerPlanCommand(root *cobra.Command) {
	root.AddCommand(planCmd)

	planCmd.Flags().StringVarP(&intentFile, "intent", "i", "intent.yaml", "Intent file path")
	planCmd.Flags().StringVarP(&outputFile, "output", "o", "plan.json", "Output plan file path")
	planCmd.Flags().StringVarP(&outputFormat, "format", "f", "json", "Output format (json/yaml)")
	planCmd.Flags().BoolVar(&debugMode, "debug", false, "Enable debug output")
	planCmd.Flags().StringVarP(&environment, "env", "e", "", "Filter by environment (optional)")
	planCmd.Flags().StringVarP(&viewPlan, "view", "v", "", "View plan (dag/dependencies/component=NAME)")
	planCmd.Flags().BoolVar(&changedOnly, "changed", false, "Show only changed components (requires git)")
	planCmd.Flags().StringVar(&baseBranch, "base", "main", "Base branch for change detection (default: main)")
	planCmd.MarkFlagRequired("config-dir")
}
