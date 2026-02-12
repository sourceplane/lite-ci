package schema

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

// Validator handles JSON schema validation
type Validator struct {
	intentSchema *jsonschema.Schema
	jobsSchema   *jsonschema.Schema
	planSchema   *jsonschema.Schema
}

// NewValidator creates a new schema validator
func NewValidator(schemasDir string) (*Validator, error) {
	v := &Validator{}

	// Load intent schema
	intentSchema, err := loadSchema(fmt.Sprintf("%s/intent.schema.yaml", schemasDir))
	if err != nil {
		return nil, fmt.Errorf("failed to load intent schema: %w", err)
	}
	v.intentSchema = intentSchema

	// Load jobs schema
	jobsSchema, err := loadSchema(fmt.Sprintf("%s/jobs.schema.yaml", schemasDir))
	if err != nil {
		return nil, fmt.Errorf("failed to load jobs schema: %w", err)
	}
	v.jobsSchema = jobsSchema

	// Load plan schema
	planSchema, err := loadSchema(fmt.Sprintf("%s/plan.schema.yaml", schemasDir))
	if err != nil {
		return nil, fmt.Errorf("failed to load plan schema: %w", err)
	}
	v.planSchema = planSchema

	return v, nil
}

// ValidateIntent validates an intent document against the schema
func (v *Validator) ValidateIntent(data interface{}) error {
	if v.intentSchema == nil {
		return fmt.Errorf("intent schema not loaded")
	}
	return v.intentSchema.Validate(data)
}

// ValidateJobRegistry validates a job registry document
func (v *Validator) ValidateJobRegistry(data interface{}) error {
	if v.jobsSchema == nil {
		return fmt.Errorf("jobs schema not loaded")
	}
	return v.jobsSchema.Validate(data)
}

// ValidatePlan validates a plan document
func (v *Validator) ValidatePlan(data interface{}) error {
	if v.planSchema == nil {
		return fmt.Errorf("plan schema not loaded")
	}
	return v.planSchema.Validate(data)
}

// loadSchema loads and compiles a schema file (JSON or YAML)
func loadSchema(path string) (*jsonschema.Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file: %w", err)
	}

	// Parse YAML to interface{} (supports both YAML and JSON)
	var schemaData interface{}
	if err := yaml.Unmarshal(data, &schemaData); err != nil {
		return nil, fmt.Errorf("failed to parse schema file: %w", err)
	}

	// Convert to JSON for schema compiler
	jsonData, err := json.Marshal(schemaData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	schema, err := jsonschema.CompileString(string(jsonData), "")
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema: %w", err)
	}

	return schema, nil
}
