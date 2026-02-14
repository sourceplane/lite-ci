package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sourceplane/liteci/internal/model"
	"github.com/sourceplane/liteci/internal/runner"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	runPlanFile string
	runExecute  bool
	runWorkDir  string
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute a compiled plan",
	Long:  "Execute the jobs and steps from a generated plan file, similar to an apply phase.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPlan()
	},
}

func registerRunCommand(root *cobra.Command) {
	root.AddCommand(runCmd)

	runCmd.Flags().StringVarP(&runPlanFile, "plan", "p", "plan.json", "Path to plan file (json or yaml)")
	runCmd.Flags().BoolVarP(&runExecute, "execute", "x", false, "Actually execute commands (default is dry-run)")
	runCmd.Flags().StringVar(&runWorkDir, "workdir", ".", "Base working directory for relative job paths")
}

func runPlan() error {
	plan, err := loadPlan(runPlanFile)
	if err != nil {
		return err
	}

	dryRun := !runExecute
	if dryRun {
		fmt.Println("□ Dry-run mode enabled. Use --execute to run commands.")
	}

	r := runner.NewRunner(runWorkDir, os.Stdout, os.Stderr, dryRun)
	if err := r.Run(plan); err != nil {
		return err
	}

	if dryRun {
		fmt.Println("✓ Dry-run complete")
	} else {
		fmt.Println("✓ Run complete")
	}

	return nil
}

func loadPlan(path string) (*model.Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read plan file %s: %w", path, err)
	}

	var plan model.Plan
	ext := filepath.Ext(path)
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &plan); err != nil {
			return nil, fmt.Errorf("failed to parse YAML plan: %w", err)
		}
	default:
		if err := json.Unmarshal(data, &plan); err != nil {
			if yamlErr := yaml.Unmarshal(data, &plan); yamlErr != nil {
				return nil, fmt.Errorf("failed to parse plan file as JSON or YAML: %w", err)
			}
		}
	}

	if len(plan.Jobs) == 0 {
		return nil, fmt.Errorf("plan contains no jobs")
	}

	return &plan, nil
}
