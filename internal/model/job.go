package model

// JobRegistry holds all job definitions (k8s-style declarative format)
type JobRegistry struct {
	APIVersion string      `yaml:"apiVersion" json:"apiVersion"`
	Kind       string      `yaml:"kind" json:"kind"`
	Metadata   Metadata    `yaml:"metadata" json:"metadata"`
	Jobs       []JobSpec   `yaml:"jobs" json:"jobs"`
}

// JobSpec defines a complete job specification with multiple steps
type JobSpec struct {
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description" json:"description"`
	Timeout     string            `yaml:"timeout" json:"timeout"`
	Retries     int               `yaml:"retries" json:"retries"`
	Steps       []Step            `yaml:"steps" json:"steps"`
	Inputs      map[string]interface{} `yaml:"inputs" json:"inputs"`
	Labels      map[string]string `yaml:"labels" json:"labels"`
}

// Step is a single execution unit within a job
type Step struct {
	Name      string `yaml:"name" json:"name"`
	Run       string `yaml:"run" json:"run"`
	Timeout   string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Retry     int    `yaml:"retry,omitempty" json:"retry,omitempty"`
	OnFailure string `yaml:"onFailure,omitempty" json:"onFailure,omitempty"` // stop, continue
}

// JobBinding is a k8s-style declarative binding between a model and its jobs
type JobBinding struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   Metadata          `yaml:"metadata" json:"metadata"`
	Spec       JobBindingSpec    `yaml:"spec" json:"spec"`
}

// JobBindingSpec specifies which jobs are available for a model
type JobBindingSpec struct {
	Model       string       `yaml:"model" json:"model"`                 // Model name (helm, terraform, charts, etc)
	Jobs        []JobRef     `yaml:"jobs" json:"jobs"`                   // List of available jobs
	DefaultJob  string       `yaml:"defaultJob" json:"defaultJob"`       // Default job to execute
	Constraints JobConstraints `yaml:"constraints,omitempty" json:"constraints,omitempty"`
}

// JobRef is a reference to a job by name
type JobRef struct {
	Name     string `yaml:"name" json:"name"`
	Required bool   `yaml:"required,omitempty" json:"required,omitempty"` // Must be included in plan
}

// JobConstraints defines constraints for job execution
type JobConstraints struct {
	Platforms []string `yaml:"platforms,omitempty" json:"platforms,omitempty"` // kubernetes, docker, etc
	MinVersion string `yaml:"minVersion,omitempty" json:"minVersion,omitempty"` // Minimum tool version
}

// JobInstance is a materialized job for a component in an environment
type JobInstance struct {
	ID          string
	Name        string
	Component   string
	Environment string
	Composition string
	Path        string
	Steps       []RenderedStep
	DependsOn   []string
	Timeout     string
	Retries     int
	Config      map[string]interface{} // Single source of truth for env vars
	Labels      map[string]string
}

// RenderedStep is a step with all templates resolved
type RenderedStep struct {
	Name      string `json:"name"`
	Run       string `json:"run"`
	Timeout   string `json:"timeout"`
	Retry     int    `json:"retry"`
	OnFailure string `json:"onFailure"`
}

// JobGraph represents the logical DAG of all job instances
type JobGraph struct {
	Jobs  map[string]*JobInstance
	Edges map[string][]string // jobID -> [dependentJobIDs]
}
