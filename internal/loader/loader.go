package loader

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/sourceplane/liteci/internal/model"
	"gopkg.in/yaml.v3"
)

// LoadIntent loads and parses an intent YAML file
func LoadIntent(path string) (*model.Intent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read intent file: %w", err)
	}

	var intent model.Intent
	if err := yaml.Unmarshal(data, &intent); err != nil {
		return nil, fmt.Errorf("failed to parse intent YAML: %w", err)
	}

	return &intent, nil
}

// LoadJobRegistry loads and parses a job registry YAML file
func LoadJobRegistry(path string) (*model.JobRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read job registry file: %w", err)
	}

	var registry model.JobRegistry
	if err := yaml.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("failed to parse job registry YAML: %w", err)
	}

	return &registry, nil
}

// LoadJSONSchema loads a JSON schema file
func LoadJSONSchema(path string) (interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file: %w", err)
	}

	var schema interface{}
	if err := yaml.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	return schema, nil
}

// Composition holds a composition's job definitions and schema
type Composition struct {
	Name     string
	Jobs     []model.JobSpec           // All jobs for this component type
	JobMap   map[string]*model.JobSpec // Quick lookup by job name
	Schema   *jsonschema.Schema
	Bindings *model.JobBinding // Optional job binding declaration
}

// CompositionRegistry holds all loaded compositions
type CompositionRegistry struct {
	Types    map[string]*Composition
	Jobs     *model.JobRegistry // For backward compatibility
	Bindings map[string]*model.JobBinding // Model -> JobBinding mapping
}

// LoadCompositionsFromDir loads composition jobs and schemas from a config directory path.
// Supports glob patterns for recursive search:
//   - Exact path: Non-recursive, looks for job.yaml and schema.yaml in immediate subdirectories
//   - Path with *: Recursive glob pattern (single level)
//   - Path with **: Recursive glob pattern (multiple levels)
// Example paths:
//   - "runtime/config/compositions" - non-recursive: looks in {charts,helm,etc}/
//   - "runtime/config/*" - recursive: looks in all subdirectories
//   - "runtime/config/**" - recursive: looks in all nested subdirectories
func LoadCompositionsFromDir(configDir string) (*CompositionRegistry, error) {
	// Check if path contains glob patterns
	isRecursive := strings.Contains(configDir, "*")

	var searchPaths []string

	if isRecursive {
		// Glob pattern provided - use filepath.Glob
		matches, err := filepath.Glob(configDir)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate glob pattern %s: %w", configDir, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("glob pattern %s matched no directories", configDir)
		}
		searchPaths = matches
	} else {
		// Exact path - check if it exists
		info, err := os.Stat(configDir)
		if err != nil {
			return nil, fmt.Errorf("failed to access config directory %s: %w", configDir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("config path is not a directory: %s", configDir)
		}
		searchPaths = []string{configDir}
	}

	registry := &CompositionRegistry{
		Types:    make(map[string]*Composition),
		Bindings: make(map[string]*model.JobBinding),
		Jobs: &model.JobRegistry{
			APIVersion: "sourceplane.io/v1",
			Kind:       "JobRegistry",
			Jobs:       []model.JobSpec{},
		},
	}

	// Maps to track job.yaml -> schema.yaml pairs
	jobFiles := make(map[string]string)   // job.yaml path -> variant type
	schemaFiles := make(map[string]string) // variant type -> schema.yaml path

	// Process each search path
	for _, basePath := range searchPaths {
		if isRecursive {
			// For recursive search, walk the directory tree
			err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}

				if info.IsDir() {
					return nil
				}

				filename := info.Name()
				if filename != "job.yaml" && filename != "schema.yaml" {
					return nil
				}

				// Extract variant type from immediate parent directory
				parentDir := filepath.Dir(path)
				typeName := filepath.Base(parentDir)

				if filename == "job.yaml" {
					jobFiles[path] = typeName
				} else if filename == "schema.yaml" {
					schemaFiles[typeName] = path
				}

				return nil
			})

			if err != nil {
				return nil, fmt.Errorf("failed to walk directory %s: %w", basePath, err)
			}
		} else {
			// Non-recursive: only look in direct subdirectories for job.yaml and schema.yaml
			entries, err := os.ReadDir(basePath)
			if err != nil {
				return nil, fmt.Errorf("failed to read directory %s: %w", basePath, err)
			}

			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}

				typeName := entry.Name()
				typeDir := filepath.Join(basePath, typeName)

				// Check for job.yaml in this subdirectory
				jobPath := filepath.Join(typeDir, "job.yaml")
				if _, err := os.Stat(jobPath); err == nil {
					jobFiles[jobPath] = typeName
				}

				// Check for schema.yaml in this subdirectory
				schemaPath := filepath.Join(typeDir, "schema.yaml")
				if _, err := os.Stat(schemaPath); err == nil {
					schemaFiles[typeName] = schemaPath
				}
			}
		}
	}

	if len(jobFiles) == 0 {
		return nil, fmt.Errorf("no job.yaml files found in config path: %s", configDir)
	}

	// Process each job.yaml and match with its schema.yaml
	for jobPath, typeName := range jobFiles {
		schemaPath, schemaExists := schemaFiles[typeName]
		if !schemaExists {
			return nil, fmt.Errorf("missing schema.yaml for job registry type %s (job at %s)", typeName, jobPath)
		}

		// Load job registry definition
		jobData, err := os.ReadFile(jobPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read job definition for type %s: %w", typeName, err)
		}

		var jobRegistry model.JobRegistry
		if err := yaml.Unmarshal(jobData, &jobRegistry); err != nil {
			return nil, fmt.Errorf("failed to parse job registry definition for type %s: %w", typeName, err)
		}

		if len(jobRegistry.Jobs) == 0 {
			return nil, fmt.Errorf("no jobs defined in job registry for type %s", typeName)
		}

		// Load schema definition
		schemaData, err := os.ReadFile(schemaPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read schema definition for type %s: %w", typeName, err)
		}

		// Parse YAML to interface{} (supports both YAML and JSON)
		var schemaObj interface{}
		if err := yaml.Unmarshal(schemaData, &schemaObj); err != nil {
			return nil, fmt.Errorf("failed to parse schema file for type %s: %w", typeName, err)
		}

		// Convert to JSON for schema compiler
		jsonData, err := json.Marshal(schemaObj)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal schema for type %s: %w", typeName, err)
		}

		// Compile schema with proper URI and custom LoadURL
		schemaURI := fmt.Sprintf("profiles://%s/schema.json", typeName)
		compiler := jsonschema.NewCompiler()
		compiler.LoadURL = func(url string) (io.ReadCloser, error) {
			// Return the schema we just read
			if url == schemaURI {
				return io.NopCloser(strings.NewReader(string(jsonData))), nil
			}
			// For other URLs, we'll just return an error
			return nil, fmt.Errorf("external schema reference not supported: %s", url)
		}

		schema, err := compiler.Compile(schemaURI)
		if err != nil {
			return nil, fmt.Errorf("failed to compile schema for type %s: %w", typeName, err)
		}

		// Store in registry with job map for quick lookup
		composition := &Composition{
			Name:   typeName,
			Jobs:   jobRegistry.Jobs,
			JobMap: make(map[string]*model.JobSpec),
			Schema: schema,
		}

		// Build job map for quick lookup by name
		for i := range jobRegistry.Jobs {
			composition.JobMap[jobRegistry.Jobs[i].Name] = &jobRegistry.Jobs[i]
		}

		registry.Types[typeName] = composition

		// Also add jobs to the registry's job list for backward compatibility
		registry.Jobs.Jobs = append(registry.Jobs.Jobs, jobRegistry.Jobs...)
	}

	if len(registry.Types) == 0 {
		return nil, fmt.Errorf("no component type jobs found in config path: %s", configDir)
	}

	return registry, nil
}

// ValidateComponentAgainstComposition validates a component against its composition schema
func (reg *CompositionRegistry) ValidateComponentAgainstComposition(component *model.Component) error {
	composition, exists := reg.Types[component.Type]
	if !exists {
		return fmt.Errorf("component type not found: %s", component.Type)
	}

	if composition.Schema == nil {
		return fmt.Errorf("schema not loaded for component type: %s", component.Type)
	}

	// Build validation object with component properties
	validationObj := map[string]interface{}{
		"name":   component.Name,
		"type":   component.Type,
		"inputs": component.Inputs,
		"domain": component.Domain,
		"labels": component.Labels,
	}

	if err := composition.Schema.Validate(validationObj); err != nil {
		return fmt.Errorf("component %s failed validation against type %s: %w", component.Name, component.Type, err)
	}

	return nil
}

// ValidateAllComponents validates all components in a normalized intent
func (reg *CompositionRegistry) ValidateAllComponents(normalized *model.NormalizedIntent) error {
	for _, comp := range normalized.Components {
		if err := reg.ValidateComponentAgainstComposition(&comp); err != nil {
			return err
		}
	}
	return nil
}
