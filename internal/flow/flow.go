// Package flow is the orun workflow language (specs/orun-workflows-v3): a
// kind: Workflow whose steps use the same verbs as job templates and blueprint
// hooks (run: argv / action: / workflow:), pull-model `needs:` edges, CEL for
// every expression position, and typed inputs mirroring blueprint InputSpec.
//
// torkflow/v1 is rejected outright — no converter, no compat window (design
// §12, WA0 decision).
package flow

import (
	"crypto/sha256"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	APIVersion = "orun.dev/v1"
	Kind       = "Workflow"
)

// InputSpec mirrors scaffold.InputSpec (kept local to avoid an import cycle:
// scaffold will invoke flow for hook workflows in WA5).
type InputSpec struct {
	Type        string   `yaml:"type"`
	Required    bool     `yaml:"required,omitempty"`
	Default     any      `yaml:"default,omitempty"`
	Values      []string `yaml:"values,omitempty"`
	Pattern     string   `yaml:"pattern,omitempty"`
	Secret      bool     `yaml:"secret,omitempty"`
	Description string   `yaml:"description,omitempty"`
}

type Metadata struct {
	Name string `yaml:"name"`
}

type Connection struct {
	Type string `yaml:"type"`
}

type Retry struct {
	MaxRetries int    `yaml:"maxRetries"`
	BaseDelay  string `yaml:"baseDelay,omitempty"` // duration; default 1s
	Kind       string `yaml:"kind,omitempty"`      // fixed|exponential (default exponential)
}

type Poll struct {
	Interval string `yaml:"interval"`
	Timeout  string `yaml:"timeout"`
	Until    string `yaml:"until"`
}

type Step struct {
	Name            string            `yaml:"name"`
	Needs           []string          `yaml:"needs,omitempty"`
	If              string            `yaml:"if,omitempty"`
	Run             []string          `yaml:"run,omitempty"`
	Action          string            `yaml:"action,omitempty"`
	Workflow        string            `yaml:"workflow,omitempty"`
	With            map[string]any    `yaml:"with,omitempty"`
	Env             map[string]string `yaml:"env,omitempty"`
	Cwd             string            `yaml:"cwd,omitempty"`
	Connection      string            `yaml:"connection,omitempty"`
	Timeout         string            `yaml:"timeout,omitempty"`
	Retry           *Retry            `yaml:"retry,omitempty"`
	ContinueOnError bool              `yaml:"continueOnError,omitempty"`
	Poll            *Poll             `yaml:"poll,omitempty"`
	Outputs         map[string]string `yaml:"outputs,omitempty"`
}

// Verb reports which of the three execution verbs the step uses.
func (s Step) Verb() string {
	switch {
	case len(s.Run) > 0:
		return "run"
	case s.Action != "":
		return "action"
	case s.Workflow != "":
		return "workflow"
	}
	return ""
}

type Workflow struct {
	APIVersion  string                `yaml:"apiVersion"`
	Kind        string                `yaml:"kind"`
	Metadata    Metadata              `yaml:"metadata"`
	Inputs      map[string]InputSpec  `yaml:"inputs,omitempty"`
	Connections map[string]Connection `yaml:"connections,omitempty"`
	Outputs     map[string]string     `yaml:"outputs,omitempty"`
	MaxParallel int                   `yaml:"maxParallel,omitempty"`
	Steps       []Step                `yaml:"steps"`
}

var slugRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// DefaultStepTimeout applies when a step declares none (design §6: the ceiling
// is mandatory; the default is how it stays mandatory without ceremony).
const DefaultStepTimeout = 10 * time.Minute

// Load parses and fully validates a workflow file. Validation is real (WA1):
// DAG shape, verb exclusivity, action resolution, CEL compilation, and
// declared-name reference checks all happen here — not at run time.
func Load(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse validates workflow bytes. See Load.
func Parse(data []byte) (*Workflow, error) {
	var probe struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parse workflow: %w", err)
	}
	if probe.APIVersion == "torkflow/v1" {
		return nil, fmt.Errorf("torkflow/v1 workflows are not supported: author %s kind: %s instead (specs/orun-workflows-v3 design §12 — no converter ships; the §2 table is the manual mapping)", APIVersion, Kind)
	}
	if probe.APIVersion != APIVersion || probe.Kind != Kind {
		return nil, fmt.Errorf("not a workflow: want apiVersion %s kind %s, got %q %q", APIVersion, Kind, probe.APIVersion, probe.Kind)
	}

	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	var wf Workflow
	if err := dec.Decode(&wf); err != nil {
		return nil, fmt.Errorf("parse workflow: %w", err)
	}
	if err := wf.validate(); err != nil {
		return nil, err
	}
	return &wf, nil
}

// Digest is the content pin: sha256 over the raw file bytes.
func Digest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}

func (w *Workflow) validate() error {
	if !slugRe.MatchString(w.Metadata.Name) {
		return fmt.Errorf("metadata.name %q must match %s", w.Metadata.Name, slugRe)
	}
	for name := range w.Inputs {
		if !slugIdentOK(name) {
			return fmt.Errorf("inputs.%s: name must be a lower-case identifier", name)
		}
	}
	if len(w.Steps) == 0 {
		return fmt.Errorf("workflow %s declares no steps", w.Metadata.Name)
	}

	steps := map[string]*Step{}
	for i := range w.Steps {
		s := &w.Steps[i]
		if !slugRe.MatchString(s.Name) {
			return fmt.Errorf("steps[%d]: name %q must match %s", i, s.Name, slugRe)
		}
		if _, dup := steps[s.Name]; dup {
			return fmt.Errorf("duplicate step name %q", s.Name)
		}
		steps[s.Name] = s

		verbs := 0
		if len(s.Run) > 0 {
			verbs++
		}
		if s.Action != "" {
			verbs++
		}
		if s.Workflow != "" {
			verbs++
		}
		if verbs != 1 {
			return fmt.Errorf("step %s: exactly one of run/action/workflow is required (got %d)", s.Name, verbs)
		}
		if s.Verb() != "run" && (len(s.Env) > 0 || s.Cwd != "") {
			return fmt.Errorf("step %s: env/cwd apply only to run: steps", s.Name)
		}
		if s.Verb() == "run" && len(s.With) > 0 {
			return fmt.Errorf("step %s: with: applies only to action/workflow steps", s.Name)
		}
		if s.Connection != "" {
			if s.Verb() != "action" {
				return fmt.Errorf("step %s: connection applies only to action: steps", s.Name)
			}
			if _, ok := w.Connections[s.Connection]; !ok {
				return fmt.Errorf("step %s: connection %q is not declared", s.Name, s.Connection)
			}
		}
		if s.Action != "" {
			if _, err := ResolveAction(s.Action); err != nil {
				return fmt.Errorf("step %s: %w", s.Name, err)
			}
		}
		if s.Timeout != "" {
			if _, err := time.ParseDuration(s.Timeout); err != nil {
				return fmt.Errorf("step %s: timeout: %w", s.Name, err)
			}
		}
		if s.Retry != nil {
			if s.Retry.MaxRetries < 1 {
				return fmt.Errorf("step %s: retry.maxRetries must be >= 1", s.Name)
			}
			if s.Retry.BaseDelay != "" {
				if _, err := time.ParseDuration(s.Retry.BaseDelay); err != nil {
					return fmt.Errorf("step %s: retry.baseDelay: %w", s.Name, err)
				}
			}
			switch s.Retry.Kind {
			case "", "fixed", "exponential":
			default:
				return fmt.Errorf("step %s: retry.kind must be fixed|exponential", s.Name)
			}
		}
		if s.Poll != nil && s.Retry != nil {
			return fmt.Errorf("step %s: poll and retry are mutually exclusive — the poll loop subsumes retry (design §7)", s.Name)
		}
		if s.Poll != nil {
			for f, v := range map[string]string{"interval": s.Poll.Interval, "timeout": s.Poll.Timeout} {
				d, err := time.ParseDuration(v)
				if err != nil || d <= 0 {
					return fmt.Errorf("step %s: poll.%s must be a positive duration", s.Name, f)
				}
			}
			if strings.TrimSpace(s.Poll.Until) == "" {
				return fmt.Errorf("step %s: poll.until is required", s.Name)
			}
		}
	}

	// needs resolve + acyclicity (Kahn).
	indeg := map[string]int{}
	for name := range steps {
		indeg[name] = 0
	}
	for _, s := range w.Steps {
		seen := map[string]bool{}
		for _, n := range s.Needs {
			if _, ok := steps[n]; !ok {
				return fmt.Errorf("step %s: needs unknown step %q", s.Name, n)
			}
			if n == s.Name {
				return fmt.Errorf("step %s: needs itself", s.Name)
			}
			if seen[n] {
				return fmt.Errorf("step %s: duplicate needs %q", s.Name, n)
			}
			seen[n] = true
			indeg[s.Name]++
		}
	}
	queue := []string{}
	for n, d := range indeg {
		if d == 0 {
			queue = append(queue, n)
		}
	}
	done := 0
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		done++
		for _, s := range w.Steps {
			for _, dep := range s.Needs {
				if dep == n {
					indeg[s.Name]--
					if indeg[s.Name] == 0 {
						queue = append(queue, s.Name)
					}
				}
			}
		}
	}
	if done != len(steps) {
		return fmt.Errorf("workflow %s: needs edges form a cycle", w.Metadata.Name)
	}

	// Expressions: compile every CEL position and check declared-name refs.
	return w.validateExpressions()
}

func slugIdentOK(s string) bool {
	return regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`).MatchString(s)
}
