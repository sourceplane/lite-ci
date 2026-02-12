package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sourceplane/liteci/internal/expand"
	"github.com/sourceplane/liteci/internal/loader"
	"github.com/sourceplane/liteci/internal/model"
	"github.com/sourceplane/liteci/internal/normalize"
	"github.com/sourceplane/liteci/internal/planner"
	"github.com/sourceplane/liteci/internal/render"
	"gopkg.in/yaml.v3"
)

var (
	intentFile     string
	configDir      string
	outputFile     string
	outputFormat   string
	debugMode      bool
	environment    string
	longFormat     bool
	expandJobs     bool
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

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Generate execution plan from intent",
	RunE: func(cmd *cobra.Command, args []string) error {
		return generatePlan()
	},
}

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate intent and jobs YAML",
	RunE: func(cmd *cobra.Command, args []string) error {
		return validateFiles()
	},
}

var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Debug intent processing",
	RunE: func(cmd *cobra.Command, args []string) error {
		return debugIntent()
	},
}

var variantsCmd = &cobra.Command{
	Use:     "variants [variant]",
	Aliases: []string{"variant"},
	Short:   "Manage variants",
	Long:    "List and inspect available variants. Use 'liteci variants' to list all, or 'liteci variants <name>' for details.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listVariants(args)
	},
}

var variantsListCmd = &cobra.Command{
	Use:   "list [variant]",
	Short: "List available variants",
	Long:  "List available variants with descriptions and fields. Optionally specify a variant for detailed information.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listVariants(args)
	},
}

func init() {
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(debugCmd)
	rootCmd.AddCommand(variantsCmd)

	variantsCmd.AddCommand(variantsListCmd)

	// Global flags (available to all commands)
	rootCmd.PersistentFlags().StringVarP(&configDir, "config-dir", "c", "", "Config directory for JobRegistry definitions (use * or ** for recursive scanning)")
	rootCmd.MarkPersistentFlagRequired("config-dir")

	// Command-specific flags
	planCmd.Flags().StringVarP(&intentFile, "intent", "i", "intent.yaml", "Intent file path")
	planCmd.Flags().StringVarP(&outputFile, "output", "o", "plan.json", "Output plan file path")
	planCmd.Flags().StringVarP(&outputFormat, "format", "f", "json", "Output format (json/yaml)")
	planCmd.Flags().BoolVar(&debugMode, "debug", false, "Enable debug output")
	planCmd.Flags().StringVarP(&environment, "env", "e", "", "Filter by environment (optional)")

	validateCmd.Flags().StringVarP(&intentFile, "intent", "i", "intent.yaml", "Intent file path")
	validateCmd.Flags().BoolVar(&debugMode, "debug", false, "Enable debug output")

	debugCmd.Flags().StringVarP(&intentFile, "intent", "i", "intent.yaml", "Intent file path")

	variantsListCmd.Flags().BoolVarP(&longFormat, "long", "l", false, "Show detailed information")
	variantsListCmd.Flags().BoolVarP(&expandJobs, "expand-jobs", "e", false, "Show all job steps and details (with -l)")

	variantsCmd.Flags().BoolVarP(&expandJobs, "expand-jobs", "e", false, "Show all job steps and details")
}

func generatePlan() error {
	fmt.Println("□ Loading intent...")
	intent, err := loader.LoadIntent(intentFile)
	if err != nil {
		return fmt.Errorf("failed to load intent: %w", err)
	}

	fmt.Println("□ Loading variants...")
	variantRegistry, err := loader.LoadVariantsFromDir(configDir)
	if err != nil {
		return fmt.Errorf("failed to load profiles from %s: %w", configDir, err)
	}

	// Build VariantInfo map for the planner with default jobs
	variantInfos := make(map[string]*planner.VariantInfo)
	for typeName, variant := range variantRegistry.Types {
		// Use first job as default if available
		var defaultJob *model.JobSpec
		if len(variant.Jobs) > 0 {
			defaultJob = &variant.Jobs[0]
		}
		variantInfos[typeName] = &planner.VariantInfo{
			Type:       typeName,
			DefaultJob: defaultJob,
		}
	}

	fmt.Println("□ Normalizing intent...")
	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		return fmt.Errorf("failed to normalize intent: %w", err)
	}

	fmt.Println("□ Validating components against variant schemas...")
	if err := variantRegistry.ValidateAllComponents(normalized); err != nil {
		return fmt.Errorf("component validation failed: %w", err)
	}

	fmt.Println("□ Expanding (env × component)...")
	expander := expand.NewExpander(normalized)
	instances, err := expander.Expand()
	if err != nil {
		return fmt.Errorf("failed to expand intent: %w", err)
	}

	if debugMode {
		count := 0
		for _, envInsts := range instances {
			count += len(envInsts)
		}
		fmt.Printf("  Generated %d component instances\n", count)
	}

	fmt.Println("□ Binding jobs and resolving dependencies...")
	jobPlanner := planner.NewJobPlanner(variantInfos)
	jobInstances, err := jobPlanner.PlanJobs(instances)
	if err != nil {
		return fmt.Errorf("failed to plan jobs: %w", err)
	}

	fmt.Println("□ Detecting cycles...")
	dag := planner.NewJobGraph(jobInstances)
	if err := dag.DetectCycles(); err != nil {
		return fmt.Errorf("cycle detection failed: %w", err)
	}

	fmt.Println("□ Topologically sorting...")
	sorted, err := dag.TopologicalSort()
	if err != nil {
		return fmt.Errorf("topological sort failed: %w", err)
	}

	if debugMode {
		fmt.Printf("  Sorted %d jobs\n", len(sorted))
	}

	fmt.Println("□ Rendering plan...")
	
	// Build JobRegistry bindings map (model -> JobRegistry name)
	// Find all job.yaml files recursively and extract metadata
	jobBindings := make(map[string]string)
	filepath.Walk(configDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != "job.yaml" {
			return nil
		}

		// Extract variant type from path structure (parent directory of job.yaml)
		relPath, err := filepath.Rel(configDir, path)
		if err != nil {
			return nil
		}

		pathDir := filepath.Dir(relPath)
		pathParts := strings.Split(pathDir, string(filepath.Separator))
		if len(pathParts) < 1 {
			return nil
		}

		typeName := pathParts[len(pathParts)-1]

		// Try to read JobRegistry metadata
		jobData, err := os.ReadFile(path)
		if err == nil {
			var jobRegistry map[string]interface{}
			if err := yaml.Unmarshal(jobData, &jobRegistry); err == nil {
				if metadata, ok := jobRegistry["metadata"].(map[string]interface{}); ok {
					if name, ok := metadata["name"].(string); ok {
						jobBindings[typeName] = name
					}
				}
			}
		}
		return nil
	})
	
	renderer := render.NewRenderer()
	plan := renderer.RenderPlan(intent.Metadata, jobInstances, jobBindings)

	if debugMode {
		fmt.Println("\n" + renderer.DebugDump(plan))
	}

	// Write plan to file
	if err := renderer.WritePlan(plan, outputFile); err != nil {
		return fmt.Errorf("failed to write plan: %w", err)
	}

	fmt.Printf("✓ Plan generated with %d jobs\n", len(plan.Jobs))
	fmt.Printf("✓ Saved to: %s\n", outputFile)
	return nil
}

func validateFiles() error {
	fmt.Println("□ Validating intent...")
	intent, err := loader.LoadIntent(intentFile)
	if err != nil {
		return fmt.Errorf("failed to load intent: %w", err)
	}

	fmt.Println("✓ Intent is valid")

	fmt.Println("□ Normalizing intent...")
	_, err = normalize.NormalizeIntent(intent)
	if err != nil {
		return fmt.Errorf("normalization failed: %w", err)
	}

	fmt.Println("✓ All validation passed")
	return nil
}

func debugIntent() error {
	fmt.Println("□ Loading and normalizing...")
	intent, err := loader.LoadIntent(intentFile)
	if err != nil {
		return err
	}

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		return err
	}

	fmt.Printf("\nMetadata: %+v\n", normalized.Metadata)
	fmt.Printf("Groups: %d\n", len(normalized.Groups))
	for name, group := range normalized.Groups {
		fmt.Printf("  - %s: policies=%v, defaults=%v\n", name, group.Policies, group.Defaults)
	}

	fmt.Printf("Environments: %d\n", len(normalized.Environments))
	for name, env := range normalized.Environments {
		fmt.Printf("  - %s: %d components, policies=%v\n", name, len(env.Selectors.Components), env.Policies)
	}

	fmt.Printf("Components: %d\n", len(normalized.Components))
	for name, comp := range normalized.Components {
		fmt.Printf("  - %s: type=%s, domain=%s, enabled=%v, deps=%d\n", 
			name, comp.Type, comp.Domain, comp.Enabled, len(comp.DependsOn))
	}

	return nil
}

func listVariants(args []string) error {
	variantRegistry, err := loader.LoadVariantsFromDir(configDir)
	if err != nil {
		return fmt.Errorf("failed to load variants from %s: %w", configDir, err)
	}

	// If a specific variant is requested, show detailed info
	if len(args) > 0 {
		variantName := args[0]
		variant, exists := variantRegistry.Types[variantName]
		if !exists {
			return fmt.Errorf("variant not found: %s", variantName)
		}

		info, err := ExtractModelInfo(variantName, variant, configDir)
		if err != nil {
			return fmt.Errorf("failed to extract variant info: %w", err)
		}

		PrintLongFormat(info, expandJobs)
		return nil
	}

	// List all variants
	fmt.Println("Available Variants:")

	// Sort variant names for consistent output
	var variantNames []string
	for variantName := range variantRegistry.Types {
		variantNames = append(variantNames, variantName)
	}
	sort.Strings(variantNames)

	// Print header
	if longFormat {
		// Long format - show each variant's full details
		for _, variantName := range variantNames {
			variant := variantRegistry.Types[variantName]
			info, _ := ExtractModelInfo(variantName, variant, configDir)
			PrintLongFormat(info, expandJobs)
		}
	} else {
		// Short format - just names and job descriptions
		for _, variantName := range variantNames {
			variant := variantRegistry.Types[variantName]
			if len(variant.Jobs) > 0 {
				fmt.Printf("  %s\n", variantName)
			}
		}
	}

	if !longFormat {
		fmt.Println("\nRun 'liteci variant <name>' for detailed information")
	}

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
