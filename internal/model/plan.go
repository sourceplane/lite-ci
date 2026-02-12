package model

// Plan is the final execution-ready workflow DAG
type Plan struct {
	APIVersion string              `json:"apiVersion"`
	Kind       string              `json:"kind"`
	Metadata   Metadata            `json:"metadata"`
	Spec       PlanSpec            `json:"spec"`
	Jobs       []PlanJob           `json:"jobs"`
}

// PlanSpec holds specification about the plan and its bindings
type PlanSpec struct {
	JobBindings map[string]string `json:"jobBindings"` // model -> JobRegistry name mapping
}

// PlanJob is the execution unit in the final plan
type PlanJob struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Component   string                 `json:"component"`
	Environment string                 `json:"environment"`
	Composition string                 `json:"composition"`
	JobRegistry string                 `json:"jobRegistry"`          // Name of the JobRegistry used
	Job         string                 `json:"job"`                  // Specific job from registry
	Steps       []PlanStep             `json:"steps"`
	DependsOn   []string               `json:"dependsOn"`
	Timeout     string                 `json:"timeout"`
	Retries     int                    `json:"retries"`
	Env         map[string]interface{} `json:"env"`
	Labels      map[string]string      `json:"labels"`
	Config      map[string]interface{} `json:"config"`
}

// PlanStep is a step in the final plan
type PlanStep struct {
	Name      string `json:"name"`
	Run       string `json:"run"`
	Timeout   string `json:"timeout,omitempty"`
	Retry     int    `json:"retry,omitempty"`
	OnFailure string `json:"onFailure,omitempty"`
}
