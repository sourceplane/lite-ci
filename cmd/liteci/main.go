package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sourceplane/liteci/internal/expand"
	"github.com/sourceplane/liteci/internal/git"
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
	viewPlan       string
	changedOnly    bool
	baseBranch     string
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

var compositionsCmd = &cobra.Command{
	Use:     "compositions [composition]",
	Aliases: []string{"composition"},
	Short:   "Manage compositions",
	Long:    "List and inspect available compositions. Use 'liteci compositions' to list all, or 'liteci compositions <name>' for details.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listCompositions(args)
	},
}

var compositionsListCmd = &cobra.Command{
	Use:   "list [composition]",
	Short: "List available compositions",
	Long:  "List available compositions with descriptions and fields. Optionally specify a composition for detailed information.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listCompositions(args)
	},
}

var componentCmd = &cobra.Command{
	Use:     "component [component-name]",
	Aliases: []string{"components"},
	Short:   "List and analyze components",
	Long:    "List all components with their merged properties. Use 'liteci component <name>' for details.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listComponents(args)
	},
}

func init() {
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(debugCmd)
	rootCmd.AddCommand(compositionsCmd)
	rootCmd.AddCommand(componentCmd)

	compositionsCmd.AddCommand(compositionsListCmd)

	// Global flags (available to all commands)
	rootCmd.PersistentFlags().StringVarP(&configDir, "config-dir", "c", "", "Config directory for JobRegistry definitions (use * or ** for recursive scanning)")
	rootCmd.MarkPersistentFlagRequired("config-dir")

	// Command-specific flags
	planCmd.Flags().StringVarP(&intentFile, "intent", "i", "intent.yaml", "Intent file path")
	planCmd.Flags().StringVarP(&outputFile, "output", "o", "plan.json", "Output plan file path")
	planCmd.Flags().StringVarP(&outputFormat, "format", "f", "json", "Output format (json/yaml)")
	planCmd.Flags().BoolVar(&debugMode, "debug", false, "Enable debug output")
	planCmd.Flags().StringVarP(&environment, "env", "e", "", "Filter by environment (optional)")
	planCmd.Flags().StringVarP(&viewPlan, "view", "v", "", "View plan (dag/dependencies/component=NAME)")
	planCmd.Flags().BoolVar(&changedOnly, "changed", false, "Show only changed components (requires git)")
	planCmd.Flags().StringVar(&baseBranch, "base", "main", "Base branch for change detection (default: main)")

	validateCmd.Flags().StringVarP(&intentFile, "intent", "i", "intent.yaml", "Intent file path")
	validateCmd.Flags().BoolVar(&debugMode, "debug", false, "Enable debug output")

	debugCmd.Flags().StringVarP(&intentFile, "intent", "i", "intent.yaml", "Intent file path")

	componentCmd.Flags().StringVarP(&intentFile, "intent", "i", "intent.yaml", "Intent file path")
	componentCmd.Flags().BoolVar(&changedOnly, "changed", false, "Show only changed components (requires git)")
	componentCmd.Flags().StringVar(&baseBranch, "base", "main", "Base branch for change detection (default: main)")
	componentCmd.Flags().BoolVarP(&longFormat, "long", "l", false, "Show detailed information")

	compositionsListCmd.Flags().BoolVarP(&longFormat, "long", "l", false, "Show detailed information")
	compositionsListCmd.Flags().BoolVarP(&expandJobs, "expand-jobs", "e", false, "Show all job steps and details (with -l)")

	compositionsCmd.Flags().BoolVarP(&expandJobs, "expand-jobs", "e", false, "Show all job steps and details")
}

func generatePlan() error {
	fmt.Println("□ Loading intent...")
	intent, err := loader.LoadIntent(intentFile)
	if err != nil {
		return fmt.Errorf("failed to load intent: %w", err)
	}

	fmt.Println("□ Loading compositions...")
	compositionRegistry, err := loader.LoadCompositionsFromDir(configDir)
	if err != nil {
		return fmt.Errorf("failed to load compositions from %s: %w", configDir, err)
	}

	// Build CompositionInfo map for the planner with default jobs
	compositionInfos := make(map[string]*planner.CompositionInfo)
	for typeName, composition := range compositionRegistry.Types {
		// Use first job as default if available
		var defaultJob *model.JobSpec
		if len(composition.Jobs) > 0 {
			defaultJob = &composition.Jobs[0]
		}
		compositionInfos[typeName] = &planner.CompositionInfo{
			Type:       typeName,
			DefaultJob: defaultJob,
		}
	}

	fmt.Println("□ Normalizing intent...")
	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		return fmt.Errorf("failed to normalize intent: %w", err)
	}

	fmt.Println("□ Validating components against composition schemas...")
	if err := compositionRegistry.ValidateAllComponents(normalized); err != nil {
		return fmt.Errorf("component validation failed: %w", err)
	}

	fmt.Println("□ Expanding (env × component)...")
	expander := expand.NewExpander(normalized)
	instances, err := expander.Expand()
	if err != nil {
		return fmt.Errorf("failed to expand intent: %w", err)
	}

	// Filter instances if --changed flag is set
	if changedOnly {
		changeDetector := git.NewChangeDetector(baseBranch)
		intentChanged, _ := changeDetector.IsIntentFileChanged(intentFile)

		// Build map of changed components by checking their resolved paths
		changedComps := make(map[string]bool)
		for _, comp := range normalized.Components {
			if intentChanged {
				changedComps[comp.Name] = true
			} else {
				// Use the expanded component instances to get resolved paths
				// Check if any instance of this component has a changed path
				for _, envInstances := range instances {
					for _, inst := range envInstances {
						if inst.ComponentName == comp.Name && inst.Path != "" && inst.Path != "./" {
							pathChanged, _ := changeDetector.IsPathChanged(inst.Path)
							if pathChanged {
								changedComps[comp.Name] = true
								break
							}
						}
					}
					if changedComps[comp.Name] {
						break
					}
				}
			}
		}

		// Filter instances to only changed components
		for envName := range instances {
			var filtered []*model.ComponentInstance
			for _, inst := range instances[envName] {
				if changedComps[inst.ComponentName] {
					filtered = append(filtered, inst)
				}
			}
			instances[envName] = filtered
		}
	}

	if debugMode {
		count := 0
		for _, envInsts := range instances {
			count += len(envInsts)
		}
		fmt.Printf("  Generated %d component instances\n", count)
	}

	fmt.Println("□ Binding jobs and resolving dependencies...")
	jobPlanner := planner.NewJobPlanner(compositionInfos)
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

	// Handle --view flag
	if viewPlan != "" {
		viewer := render.NewPlanViewer(plan)
		var output string

		switch {
		case viewPlan == "dag":
			output = viewer.ViewDAG()
		case viewPlan == "dependencies":
			output = viewer.ViewDependencies()
		case strings.HasPrefix(viewPlan, "component="):
			componentName := strings.TrimPrefix(viewPlan, "component=")
			output = viewer.ViewByComponent(componentName)
		default:
			// Default to DAG view
			output = viewer.ViewDAG()
		}

		fmt.Println("\n" + output)
	}

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

func listCompositions(args []string) error {
	compositionRegistry, err := loader.LoadCompositionsFromDir(configDir)
	if err != nil {
		return fmt.Errorf("failed to load compositions from %s: %w", configDir, err)
	}

	// If a specific composition is requested, show detailed info
	if len(args) > 0 {
		compositionName := args[0]
		composition, exists := compositionRegistry.Types[compositionName]
		if !exists {
			return fmt.Errorf("composition not found: %s", compositionName)
		}

		info, err := ExtractModelInfo(compositionName, composition, configDir)
		if err != nil {
			return fmt.Errorf("failed to extract composition info: %w", err)
		}

		PrintLongFormat(info, expandJobs)
		return nil
	}

	// List all compositions
	fmt.Println("Available Compositions:")

	// Sort composition names for consistent output
	var compositionNames []string
	for compositionName := range compositionRegistry.Types {
		compositionNames = append(compositionNames, compositionName)
	}
	sort.Strings(compositionNames)

	// Print header
	if longFormat {
		// Long format - show each composition's full details
		for _, compositionName := range compositionNames {
			composition := compositionRegistry.Types[compositionName]
			info, _ := ExtractModelInfo(compositionName, composition, configDir)
			PrintLongFormat(info, expandJobs)
		}
	} else {
		// Short format - just names and job descriptions
		for _, compositionName := range compositionNames {
			composition := compositionRegistry.Types[compositionName]
			if len(composition.Jobs) > 0 {
				fmt.Printf("  %s\n", compositionName)
			}
		}
	}

	if !longFormat {
		fmt.Println("\nRun 'liteci composition <name>' for detailed information")
	}

	return nil
}

func listComponents(args []string) error {
	fmt.Println("□ Loading intent...")
	intent, err := loader.LoadIntent(intentFile)
	if err != nil {
		return fmt.Errorf("failed to load intent: %w", err)
	}

	fmt.Println("□ Normalizing intent...")
	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		return fmt.Errorf("failed to normalize intent: %w", err)
	}

	// Analyze components first to get expanded/resolved paths
	analyzer := expand.NewComponentAnalyzer(normalized)
	components, err := analyzer.ListAll()
	if err != nil {
		return fmt.Errorf("failed to analyze components: %w", err)
	}

	// Initialize change detector if --changed flag is set
	var changeDetector *git.ChangeDetector
	var changedComps map[string]bool
	if changedOnly {
		changeDetector = git.NewChangeDetector(baseBranch)
		changedComps = make(map[string]bool)

		// Check intent file for changes
		intentChanged, _ := changeDetector.IsIntentFileChanged(intentFile)

		// For each component, check if any resolved paths have changed
		for _, comp := range components {
			if intentChanged {
				// If intent changed, all components are affected
				changedComps[comp.Name] = true
			} else {
				// Check each instance's resolved path
				found := false
				for _, inst := range comp.Instances {
					if inst.Path != "" && inst.Path != "./" {
						pathChanged, _ := changeDetector.IsPathChanged(inst.Path)
						if pathChanged {
							changedComps[comp.Name] = true
							found = true
							break
						}
					}
				}
				if found {
					continue
				}
			}
		}

		if len(changedComps) == 0 {
			fmt.Println("✓ No components have changed")
			return nil
		}
	}

	// Filter by specific component if requested
	if len(args) > 0 {
		componentName := args[0]
		comp, err := analyzer.GetComponentByName(componentName)
		if err != nil {
			return fmt.Errorf("failed to get component: %w", err)
		}

		if comp.Type == "" {
			return fmt.Errorf("component not found: %s", componentName)
		}

		if changedOnly && !changedComps[componentName] {
			fmt.Printf("Component %s has not changed\n", componentName)
			return nil
		}

		printComponentDetails(comp)
		return nil
	}

	// List all components (or just changed ones)
	if len(components) == 0 {
		fmt.Println("No components found")
		return nil
	}

	fmt.Println("\nComponents:")
	for _, comp := range components {
		// Skip if --changed flag and component hasn't changed
		if changedOnly && !changedComps[comp.Name] {
			continue
		}

		if longFormat {
			printComponentDetails(comp)
		} else {
			fmt.Printf("  %s (type: %s, domain: %s, enabled: %v, environments: %d)\n",
				comp.Name, comp.Type, comp.Domain, comp.Enabled, len(comp.Instances))
		}
	}

	if !longFormat {
		fmt.Println("\nRun 'liteci component <name>' for detailed information")
	}

	return nil
}

func printComponentDetails(comp *expand.ComponentMerged) {
	fmt.Printf("\n[Component] %s\n", comp.Name)
	fmt.Printf("  Type:       %s\n", comp.Type)
	fmt.Printf("  Domain:     %s\n", comp.Domain)
	fmt.Printf("  Enabled:    %v\n", comp.Enabled)

	if len(comp.Dependencies) > 0 {
		fmt.Printf("  Dependencies: %s\n", strings.Join(comp.Dependencies, ", "))
	}

	fmt.Printf("  Instances (%d):\n", len(comp.Instances))
	for _, inst := range comp.Instances {
		fmt.Printf("    [%s] path=%s\n", inst.Environment, inst.Path)
		if len(inst.Inputs) > 0 {
			fmt.Printf("      Inputs:\n")
			for k, v := range inst.Inputs {
				fmt.Printf("        %s: %v\n", k, v)
			}
		}
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
