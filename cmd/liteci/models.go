package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/liteci/internal/loader"
	"gopkg.in/yaml.v3"
)

// ModelInfo holds extracted metadata about a model
type ModelInfo struct {
	Name             string
	Title            string
	Description      string
	RequiredFields   []string
	SupportedFields  map[string]string
	JobRegistryName  string              // Name of the JobRegistry
	JobRegistryDesc  string              // Description of the JobRegistry
	AvailableJobs    []JobBindingInfo    // All available jobs in the registry
	DefaultJobName   string              // Default job name
	JobName          string              // Currently displayed job
	JobDescription   string              // Currently displayed job description
	Steps            []StepInfo
}

// JobBindingInfo holds information about a job in the registry
type JobBindingInfo struct {
	Name        string
	Description string
	Scope       string // deployment, recovery, analysis, etc
	Steps       int    // Number of steps in this job
	Timeout     string
}

// StepInfo holds information about a job step
type StepInfo struct {
	Name        string
	Description string
	Run         string
	Timeout     string
	Retry       int
}

// ExtractModelInfo extracts metadata from a loaded variant
func ExtractModelInfo(modelName string, variant *loader.Variant, configDir string) (*ModelInfo, error) {
	info := &ModelInfo{
		Name:            modelName,
		SupportedFields: make(map[string]string),
		AvailableJobs:   []JobBindingInfo{},
		Steps:           []StepInfo{},
	}

	// Extract JobRegistry metadata
	if len(variant.Jobs) > 0 {
		// Try to read job.yaml to extract JobRegistry metadata
		jobPath := filepath.Join(configDir, modelName, "job.yaml")
		jobData, err := os.ReadFile(jobPath)
		if err == nil {
			var jobRegistry map[string]interface{}
			if err := yaml.Unmarshal(jobData, &jobRegistry); err == nil {
				if metadata, ok := jobRegistry["metadata"].(map[string]interface{}); ok {
					if name, ok := metadata["name"].(string); ok {
						info.JobRegistryName = name
					}
					if desc, ok := metadata["description"].(string); ok {
						info.JobRegistryDesc = desc
					}
				}
			}
		}

		// Build list of all available jobs from the registry
		for i, job := range variant.Jobs {
			scope := ""
			if len(job.Labels) > 0 {
				if s, ok := job.Labels["scope"]; ok {
					scope = s
				}
			}
			
			bindingInfo := JobBindingInfo{
				Name:        job.Name,
				Description: job.Description,
				Scope:       scope,
				Steps:       len(job.Steps),
				Timeout:     job.Timeout,
			}
			info.AvailableJobs = append(info.AvailableJobs, bindingInfo)
			
			// First job is the default
			if i == 0 {
				info.DefaultJobName = job.Name
			}
		}
	}

	// Extract schema metadata
	if variant.Schema != nil {
		info.Title = fmt.Sprintf("%s Model", strings.ToTitle(strings.ToLower(modelName)))
		info.Description = fmt.Sprintf("Model: %s", modelName)

		// Try to read schema file to extract required fields and field descriptions
		schemaPath := filepath.Join(configDir, modelName, "schema.yaml")
		schemaData, err := os.ReadFile(schemaPath)
		if err == nil {
			var schemaObj map[string]interface{}
			if err := yaml.Unmarshal(schemaData, &schemaObj); err == nil {
				// Extract required fields
				if required, ok := schemaObj["required"]; ok {
					if reqList, ok := required.([]interface{}); ok {
						for _, v := range reqList {
							info.RequiredFields = append(info.RequiredFields, fmt.Sprintf("%v", v))
						}
					}
				}

				// Extract supported fields from properties
				if props, ok := schemaObj["properties"]; ok {
					if propMap, ok := props.(map[string]interface{}); ok {
						for fieldName, fieldSchema := range propMap {
							if fieldMap, ok := fieldSchema.(map[string]interface{}); ok {
								if desc, ok := fieldMap["description"]; ok {
									info.SupportedFields[fieldName] = fmt.Sprintf("%v", desc)
								} else {
									info.SupportedFields[fieldName] = ""
								}
							}
						}
					}
				}
			}
		}
	}

	// Extract job metadata (from first job / default job)
	if len(variant.Jobs) > 0 {
		job := &variant.Jobs[0] // Use first job as default
		info.JobName = job.Name
		info.JobDescription = job.Description

		// Extract steps from first job
		for _, step := range job.Steps {
			stepInfo := StepInfo{
				Name:        step.Name,
				Description: step.Name,
				Run:         step.Run,
				Timeout:     step.Timeout,
				Retry:       step.Retry,
			}
			info.Steps = append(info.Steps, stepInfo)
		}
	}

	return info, nil
}

// GetSupportedFields extracts supported fields from schema properties
func GetSupportedFields(schema map[string]interface{}) map[string]string {
	supported := make(map[string]string)

	if props, ok := schema["properties"]; ok {
		if propMap, ok := props.(map[string]interface{}); ok {
			for fieldName, fieldSchema := range propMap {
				if fieldMap, ok := fieldSchema.(map[string]interface{}); ok {
					if desc, ok := fieldMap["description"]; ok {
						supported[fieldName] = fmt.Sprintf("%v", desc)
					} else {
						supported[fieldName] = ""
					}
				}
			}
		}
	}

	return supported
}

// PrintShortFormat prints model info in short format
func PrintShortFormat(info *ModelInfo) {
	fmt.Printf("%-20s  %s\n", info.Name, info.JobDescription)
}

// PrintLongFormat prints variant info in long format
func PrintLongFormat(info *ModelInfo, expandJobs bool) {
	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("Variant: %s\n", info.Name)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	if info.Description != "" {
		fmt.Printf("Description:\n  %s\n\n", info.Description)
	}

	// JobRegistry Binding
	fmt.Printf("JobRegistry Binding:\n")
	if info.JobRegistryName != "" {
		fmt.Printf("  Registry Name: %s\n", info.JobRegistryName)
	}
	if info.JobRegistryDesc != "" {
		fmt.Printf("  Registry Desc: %s\n", info.JobRegistryDesc)
	}
	fmt.Printf("  Default Job:   %s\n", info.DefaultJobName)
	fmt.Printf("  Total Jobs:    %d\n\n", len(info.AvailableJobs))

	// Available Jobs in Registry
	fmt.Printf("Available Jobs (from JobRegistry):\n")
	for i, job := range info.AvailableJobs {
		marker := "  "
		if job.Name == info.DefaultJobName {
			marker = "★ " // Mark default job
		} else {
			marker = "  "
		}
		
		scope := ""
		if job.Scope != "" {
			scope = fmt.Sprintf(" [%s]", job.Scope)
		}
		
		fmt.Printf("%s%d. %s%s\n", marker, i+1, job.Name, scope)
		fmt.Printf("     Description: %s\n", job.Description)
		fmt.Printf("     Steps: %d | Timeout: %s\n", job.Steps, job.Timeout)
		fmt.Printf("\n")
	}

	// Only show job details if expandJobs flag is set
	if !expandJobs {
		return
	}

	// Current Job Details (showing default job) - only if expandJobs
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("Using Default Job: %s\n", info.JobName)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Job information
	fmt.Printf("Job Information:\n")
	fmt.Printf("  Name:        %s\n", info.JobName)
	fmt.Printf("  Description: %s\n\n", info.JobDescription)

	// Required fields
	if len(info.RequiredFields) > 0 {
		fmt.Printf("Required Fields:\n")
		for _, field := range info.RequiredFields {
			fmt.Printf("  • %s\n", field)
		}
		fmt.Printf("\n")
	}

	// Supported input fields
	if len(info.SupportedFields) > 0 {
		fmt.Printf("Supported Input Fields:\n")
		
		// Sort fields for consistent output
		fieldNames := make([]string, 0, len(info.SupportedFields))
		for name := range info.SupportedFields {
			fieldNames = append(fieldNames, name)
		}
		sort.Strings(fieldNames)

		for _, name := range fieldNames {
			desc := info.SupportedFields[name]
			if desc != "" {
				fmt.Printf("  • %-20s - %s\n", name, desc)
			} else {
				fmt.Printf("  • %s\n", name)
			}
		}
		fmt.Printf("\n")
	}

	// Job steps
	if len(info.Steps) > 0 {
		fmt.Printf("Job Steps (for %s job):\n", info.JobName)
		for i, step := range info.Steps {
			fmt.Printf("  %d. %s\n", i+1, step.Name)
			if step.Timeout != "" {
				fmt.Printf("     Timeout: %s\n", step.Timeout)
			}
			if step.Retry > 0 {
				fmt.Printf("     Retry: %d\n", step.Retry)
			}
			fmt.Printf("     Command: %s\n", step.Run)
			fmt.Printf("\n")
		}
	}

	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
}
