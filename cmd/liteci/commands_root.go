package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

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
	headRef      string
	changedFiles []string
	uncommitted  bool
	untracked    bool
)

var rootCmd = &cobra.Command{
	Use:   "liteci",
	Short: "Planner engine: Intent → Plan DAG",
	Long:  "liteci is a schema-driven planner that compiles policy-aware intent into deterministic execution DAGs",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if configDir == "" {
			if envConfigDir := os.Getenv("LITECI_CONFIG_DIR"); envConfigDir != "" {
				configDir = envConfigDir
			} else {
				fmt.Fprintln(os.Stderr, "⚠ warning: --config-dir not set and LITECI_CONFIG_DIR is empty; commands that need compositions may fail")
			}
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&configDir, "config-dir", "c", "", "Config directory for JobRegistry definitions (or set LITECI_CONFIG_DIR; use * or ** for recursive scanning)")

	registerPlanCommand(rootCmd)
	registerRunCommand(rootCmd)
	registerValidateCommand(rootCmd)
	registerDebugCommand(rootCmd)
	registerCompositionsCommand(rootCmd)
	registerComponentCommand(rootCmd)
}
