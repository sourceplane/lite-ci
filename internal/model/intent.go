package model

// Intent is the top-level CRD for declarative deployment
type Intent struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   Metadata          `yaml:"metadata" json:"metadata"`
	Groups     map[string]Group  `yaml:"groups" json:"groups"`
	ForEach    map[string]ForEach `yaml:"forEach" json:"forEach"`
	Components []Component       `yaml:"components" json:"components"`
}

// Metadata holds standard object metadata
type Metadata struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Namespace   string `yaml:"namespace" json:"namespace"`
}

// Group defines ownership and policy constraints
type Group struct {
	Policies map[string]interface{} `yaml:"policies" json:"policies"`
	Defaults map[string]interface{} `yaml:"defaults" json:"defaults"`
}

// ForEach defines environment runtime contexts
type ForEach struct {
	Selectors ForEachSelectors       `yaml:"selectors" json:"selectors"`
	Defaults  map[string]interface{} `yaml:"defaults" json:"defaults"`
	Policies  map[string]interface{} `yaml:"policies" json:"policies"`
}

// ForEachSelectors specifies which components apply to an environment
type ForEachSelectors struct {
	Components []string `yaml:"components" json:"components"`
	Domains    []string `yaml:"domains" json:"domains"`
}

// Component is execution-agnostic declaration
type Component struct {
	Name      string                 `yaml:"name" json:"name"`
	Type      string                 `yaml:"type" json:"type"`
	Domain    string                 `yaml:"domain" json:"domain"`
	Enabled   bool                   `yaml:"enabled" json:"enabled"`
	Inputs    map[string]interface{} `yaml:"inputs" json:"inputs"`
	Labels    map[string]string      `yaml:"labels" json:"labels"`
	DependsOn []Dependency           `yaml:"dependsOn" json:"dependsOn"`
}

// Dependency specifies inter-component execution constraints
type Dependency struct {
	Component   string `yaml:"component" json:"component"`
	Environment string `yaml:"environment" json:"environment"`
	Scope       string `yaml:"scope" json:"scope"` // same-environment, cross-environment
	Condition   string `yaml:"condition" json:"condition"` // success, always, failure
}

// NormalizedIntent is the canonical internal representation
type NormalizedIntent struct {
	Metadata       Metadata
	Groups         map[string]Group
	Environments   map[string]ForEach
	Components     map[string]Component
	ComponentIndex map[string]Component // for fast lookup
}

// ComponentInstance is the expanded form of Component for a specific environment
type ComponentInstance struct {
	ComponentName string
	Environment   string
	Type          string
	Domain        string
	Labels        map[string]string
	Inputs        map[string]interface{}
	Policies      map[string]interface{}
	DependsOn     []ResolvedDependency
	Enabled       bool
}

// ResolvedDependency is a dependency with resolved target component
type ResolvedDependency struct {
	ComponentName string
	Environment   string
	Scope         string
	Condition     string
}
