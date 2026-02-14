package main

import "github.com/spf13/cobra"

var (
	intentFile   string
	configDir    string
	outputFile   string
	outputFormat string
	debugMode    bool
	environment  string
	longFormat   bool
	expandJobs   bool
	viewPlan     string
	changedOnly  bool
	baseBranch   string
)

var rootCmd = &cobra.Command{
	Use:   "liteci",
	Short: "Planner engine: Intent → Plan DAG",
	Long:  "liteci is a schema-driven planner that compiles policy-aware intent into deterministic execution DAGs",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Global config directory override check
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&configDir, "config-dir", "c", "", "Config directory for JobRegistry definitions (use * or ** for recursive scanning)")

	registerPlanCommand(rootCmd)
	registerRunCommand(rootCmd)
	registerValidateCommand(rootCmd)
	registerDebugCommand(rootCmd)
	registerCompositionsCommand(rootCmd)
	registerComponentCommand(rootCmd)
}
