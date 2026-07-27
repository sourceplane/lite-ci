package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// The in-process engine (orun-workflows-v3 WA2). Semantics are the contract of
// record — DAG execution over `needs` AND-joins, `if:` skips, per-step
// retry/backoff, continueOnError, file-backed resume that re-executes only
// non-succeeded steps. The scheduler is event-driven: a completion channel and
// a rescan, no polling sleep.

type StepState struct {
	Name     string         `json:"name"`
	Status   string         `json:"status"` // succeeded|failed|skipped|blocked
	Outputs  map[string]any `json:"outputs,omitempty"`
	ExitCode int            `json:"exitCode,omitempty"`
	Stdout   string         `json:"stdout,omitempty"`
	Stderr   string         `json:"stderr,omitempty"`
	Error    string         `json:"error,omitempty"`
	Attempts int            `json:"attempts,omitempty"`
}

type RunResult struct {
	ExecID  string
	Status  string // succeeded|failed
	Steps   map[string]*StepState
	Outputs map[string]any
}

type RunOptions struct {
	Dir         string // base dir for nested workflow: refs (the file's dir)
	Inputs      map[string]any
	Connections map[string]map[string]string
	RunRoot     string    // default .orun/wfruns
	ExecID      string    // non-empty: resume this run (digest-checked)
	Digest      string    // workflow content digest, recorded in run state
	Log         io.Writer // step progress; nil = silent
	MaxParallel int       // default 4; workflow file may override
	visited     map[string]bool
}

// Run executes the workflow to completion or first non-continuable failure.
func Run(ctx context.Context, wf *Workflow, opts RunOptions) (*RunResult, error) {
	if opts.Log == nil {
		opts.Log = io.Discard
	}
	if opts.RunRoot == "" {
		opts.RunRoot = filepath.Join(".orun", "wfruns")
	}
	inputs, err := resolveInputs(wf, opts.Inputs)
	if err != nil {
		return nil, err
	}
	for name := range opts.Connections {
		if _, ok := wf.Connections[name]; !ok {
			return nil, fmt.Errorf("connection grant %q: workflow declares no such connection", name)
		}
	}

	states := map[string]*StepState{}
	if opts.ExecID == "" {
		opts.ExecID = fmt.Sprintf("%s-%d", wf.Metadata.Name, time.Now().UnixNano())
	} else {
		if states, err = loadRunState(opts.RunRoot, opts.ExecID, opts.Digest); err != nil {
			return nil, err
		}
		// Resume re-executes only non-succeeded steps (WX6 semantics): failed,
		// blocked and in-flight states are cleared; succeeded/skipped stand.
		for name, st := range states {
			if st.Status != "succeeded" && st.Status != "skipped" {
				delete(states, name)
			}
		}
	}
	runDir := filepath.Join(opts.RunRoot, opts.ExecID)
	if err := os.MkdirAll(filepath.Join(runDir, "steps"), 0o755); err != nil {
		return nil, err
	}
	meta := map[string]any{"workflow": wf.Metadata.Name, "digest": opts.Digest, "startedAt": time.Now().UTC().Format(time.RFC3339)}
	if err := writeJSON(filepath.Join(runDir, "metadata.json"), meta); err != nil {
		return nil, err
	}

	maxPar := opts.MaxParallel
	if wf.MaxParallel > 0 {
		maxPar = wf.MaxParallel
	}
	if maxPar <= 0 {
		maxPar = 4
	}

	byName := map[string]*Step{}
	for i := range wf.Steps {
		byName[wf.Steps[i].Name] = &wf.Steps[i]
	}

	var mu sync.Mutex
	doneCh := make(chan string, len(wf.Steps))
	running := map[string]bool{}
	sem := make(chan struct{}, maxPar)

	terminalOK := func(st *StepState, dep *Step) bool {
		if st == nil {
			return false
		}
		switch st.Status {
		case "succeeded", "skipped":
			return true
		case "failed":
			return dep != nil && dep.ContinueOnError
		}
		return false
	}

	// stepsVar renders terminal states for CEL activations.
	stepsVar := func() map[string]any {
		out := map[string]any{}
		for name, st := range states {
			out[name] = map[string]any{
				"outputs": st.Outputs, "status": st.Status,
				"failed": st.Status == "failed", "skipped": st.Status == "skipped",
				"exitCode": st.ExitCode, "stdout": st.Stdout, "stderr": st.Stderr,
			}
		}
		return out
	}

	launch := func(s *Step) {
		running[s.Name] = true
		go func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			st := executeStep(ctx, wf, s, inputs, stepsVar(), opts, runDir)
			mu.Lock()
			states[s.Name] = st
			_ = writeJSON(filepath.Join(runDir, "steps", s.Name+".json"), st)
			delete(running, s.Name)
			mu.Unlock()
			fmt.Fprintf(opts.Log, "  - %s: %s\n", s.Name, st.Status)
			doneCh <- s.Name
		}()
	}

	for {
		mu.Lock()
		launched := 0
		for i := range wf.Steps {
			s := &wf.Steps[i]
			if states[s.Name] != nil || running[s.Name] {
				continue
			}
			ready, blocked := true, false
			for _, n := range s.Needs {
				st := states[n]
				if st == nil {
					if !running[n] {
						// dependency not yet scheduled — fine, next rescan
					}
					ready = false
					continue
				}
				if !terminalOK(st, byName[s.Name]) {
					if st.Status == "failed" || st.Status == "blocked" {
						blocked = true
					}
					ready = false
				}
			}
			if blocked {
				states[s.Name] = &StepState{Name: s.Name, Status: "blocked", Error: "an upstream step failed"}
				_ = writeJSON(filepath.Join(runDir, "steps", s.Name+".json"), states[s.Name])
				fmt.Fprintf(opts.Log, "  - %s: blocked\n", s.Name)
				launched++ // state changed; force another pass
				continue
			}
			if ready {
				launch(s)
				launched++
			}
		}
		allDone := len(states) == len(wf.Steps)
		anyRunning := len(running) > 0
		mu.Unlock()
		if allDone {
			break
		}
		if launched == 0 && !anyRunning {
			return nil, fmt.Errorf("workflow %s: no runnable steps and %d unfinished — scheduler invariant broken", wf.Metadata.Name, len(wf.Steps)-len(states))
		}
		if anyRunning {
			<-doneCh
		}
	}

	res := &RunResult{ExecID: opts.ExecID, Status: "succeeded", Steps: states, Outputs: map[string]any{}}
	for _, st := range states {
		if st.Status == "failed" || st.Status == "blocked" {
			if s := byName[st.Name]; !(s != nil && s.ContinueOnError && st.Status == "failed") {
				res.Status = "failed"
			}
		}
	}
	if res.Status == "succeeded" {
		vars := map[string]any{"inputs": inputs, "steps": stepsVar()}
		for name, expr := range wf.Outputs {
			v, err := EvalExpr(expr, vars)
			if err != nil {
				return nil, fmt.Errorf("outputs.%s: %w", name, err)
			}
			res.Outputs[name] = v
		}
	}
	_ = writeJSON(filepath.Join(runDir, "result.json"), map[string]any{"status": res.Status, "outputs": res.Outputs})
	return res, nil
}

// executeStep runs one step through if-guard, retry loop and verb dispatch.
func executeStep(ctx context.Context, wf *Workflow, s *Step, inputs map[string]any, steps map[string]any, opts RunOptions, runDir string) *StepState {
	st := &StepState{Name: s.Name}
	vars := map[string]any{"inputs": inputs, "steps": steps}

	if s.If != "" {
		v, err := EvalExpr(s.If, vars)
		if err != nil {
			st.Status, st.Error = "failed", fmt.Sprintf("if: %v", err)
			return st
		}
		if ok, _ := v.(bool); !ok {
			st.Status = "skipped"
			return st
		}
	}

	attempts := 1
	baseDelay := time.Second
	if s.Retry != nil {
		attempts = s.Retry.MaxRetries + 1
		if s.Retry.BaseDelay != "" {
			baseDelay, _ = time.ParseDuration(s.Retry.BaseDelay)
		}
	}
	if s.Poll != nil {
		return pollLoop(ctx, wf, s, vars, opts, st, runDir)
	}

	var lastErr error
	for a := 1; a <= attempts; a++ {
		st.Attempts = a
		var rv map[string]any
		rv, lastErr = runVerbOnce(ctx, wf, s, vars, opts, st, runDir)
		if lastErr == nil {
			if lastErr = sealOutputs(s, rv, st); lastErr == nil {
				st.Status = "succeeded"
				return st
			}
		}
		if a < attempts {
			d := baseDelay
			if s.Retry != nil && s.Retry.Kind != "fixed" {
				d = baseDelay << (a - 1)
			}
			select {
			case <-time.After(d):
			case <-ctx.Done():
				st.Status, st.Error = "failed", ctx.Err().Error()
				return st
			}
		}
	}
	st.Status, st.Error = "failed", lastErr.Error()
	return st
}

// pollLoop re-executes the step's verb on interval until `until` evaluates
// true, the poll timeout expires, or the context dies (design §7). Every
// attempt is a recorded run fact via st.Attempts; a run: non-zero exit is an
// evaluable attempt, not a terminal fault. retry: is rejected on poll steps
// at validate time — the loop subsumes it.
func pollLoop(ctx context.Context, wf *Workflow, s *Step, vars map[string]any, opts RunOptions, st *StepState, runDir string) *StepState {
	interval, _ := time.ParseDuration(s.Poll.Interval)
	timeout, _ := time.ParseDuration(s.Poll.Timeout)
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		st.Attempts++
		rv, err := runVerbOnce(ctx, wf, s, vars, opts, st, runDir)
		lastErr = err
		terminal := false
		if err != nil {
			var xe *ExitErr
			if !errorsAs(err, &xe) {
				// Non-exit faults (unresolvable binary, bad interpolation…)
				// are still retried until the deadline: transient network
				// faults from action verbs land here too.
				terminal = false
			}
			if rv == nil {
				rv = map[string]any{}
			}
			rv["exitCode"], rv["stdout"], rv["stderr"] = st.ExitCode, st.Stdout, st.Stderr
		}
		if !terminal {
			v, uerr := EvalExpr(s.Poll.Until, merged(vars, rv))
			if uerr == nil {
				if ok, _ := v.(bool); ok {
					if serr := sealOutputs(s, rv, st); serr != nil {
						st.Status, st.Error = "failed", serr.Error()
						return st
					}
					st.Status = "succeeded"
					return st
				}
			}
		}
		if time.Now().After(deadline) {
			msg := fmt.Sprintf("poll: %s not satisfied within %s after %d attempt(s)", s.Poll.Until, s.Poll.Timeout, st.Attempts)
			if lastErr != nil {
				msg += ": last error: " + lastErr.Error()
			}
			st.Status, st.Error = "failed", msg
			return st
		}
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			st.Status, st.Error = "failed", ctx.Err().Error()
			return st
		}
	}
}

func merged(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func errorsAs(err error, target **ExitErr) bool {
	xe, ok := err.(*ExitErr)
	if ok {
		*target = xe
	}
	return ok
}

// sealOutputs evaluates the step's declared outputs over the attempt result.
func sealOutputs(s *Step, resultVars map[string]any, st *StepState) error {
	for name, expr := range s.Outputs {
		v, err := EvalExpr(expr, resultVars)
		if err != nil {
			return fmt.Errorf("outputs.%s: %w", name, err)
		}
		if st.Outputs == nil {
			st.Outputs = map[string]any{}
		}
		st.Outputs[name] = v
	}
	if out, ok := resultVars["output"].(map[string]any); ok && st.Outputs == nil {
		st.Outputs = out
	}
	return nil
}

// runVerbOnce executes the step's verb a single time and returns the result
// variables (output/exitCode/stdout/stderr) for outputs/until evaluation.
func runVerbOnce(ctx context.Context, wf *Workflow, s *Step, vars map[string]any, opts RunOptions, st *StepState, runDir string) (map[string]any, error) {
	timeout := DefaultStepTimeout
	if s.Timeout != "" {
		timeout, _ = time.ParseDuration(s.Timeout)
	}
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resultVars := map[string]any{}
	for k, v := range vars {
		resultVars[k] = v
	}

	switch s.Verb() {
	case "run":
		if err := runExec(sctx, s, vars, runDir, st); err != nil {
			return resultVars, err
		}
		resultVars["exitCode"] = st.ExitCode
		resultVars["stdout"] = st.Stdout
		resultVars["stderr"] = st.Stderr
	case "action":
		action, err := ResolveAction(s.Action)
		if err != nil {
			return resultVars, err
		}
		with, err := interpolateMap(s.With, vars)
		if err != nil {
			return resultVars, err
		}
		if err := action.Validate(with); err != nil {
			return resultVars, err
		}
		out, err := action.Invoke(sctx, with, opts.Connections[s.Connection])
		if err != nil {
			return resultVars, err
		}
		resultVars["output"] = out
	case "workflow":
		path := filepath.Join(opts.Dir, s.Workflow)
		abs, err := filepath.Abs(path)
		if err != nil {
			return resultVars, err
		}
		if opts.visited[abs] {
			return resultVars, fmt.Errorf("nested workflow cycle at %s", s.Workflow)
		}
		sub, err := Load(path)
		if err != nil {
			return resultVars, err
		}
		with, err := interpolateMap(s.With, vars)
		if err != nil {
			return resultVars, err
		}
		digest, _ := Digest(path)
		visited := map[string]bool{abs: true}
		for k := range opts.visited {
			visited[k] = true
		}
		subRes, err := Run(sctx, sub, RunOptions{
			Dir: filepath.Dir(path), Inputs: with, Connections: opts.Connections,
			RunRoot: opts.RunRoot, Digest: digest, Log: opts.Log, visited: visited,
		})
		if err != nil {
			return resultVars, err
		}
		if subRes.Status != "succeeded" {
			return resultVars, fmt.Errorf("nested workflow %s failed", s.Workflow)
		}
		resultVars["output"] = subRes.Outputs
	}
	return resultVars, nil
}

func interpolateMap(m map[string]any, vars map[string]any) (map[string]any, error) {
	out := map[string]any{}
	var walk func(v any) (any, error)
	walk = func(v any) (any, error) {
		switch t := v.(type) {
		case string:
			return Interpolate(t, vars)
		case map[string]any:
			r := map[string]any{}
			for k, vv := range t {
				w, err := walk(vv)
				if err != nil {
					return nil, err
				}
				r[k] = w
			}
			return r, nil
		case []any:
			r := make([]any, len(t))
			for i, vv := range t {
				w, err := walk(vv)
				if err != nil {
					return nil, err
				}
				r[i] = w
			}
			return r, nil
		}
		return v, nil
	}
	for k, v := range m {
		w, err := walk(v)
		if err != nil {
			return nil, fmt.Errorf("with.%s: %w", k, err)
		}
		out[k] = w
	}
	return out, nil
}

// resolveInputs applies defaults and validates required/pattern/enum.
func resolveInputs(wf *Workflow, given map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for name, spec := range wf.Inputs {
		v, ok := given[name]
		if !ok {
			if spec.Default != nil {
				out[name] = spec.Default
				continue
			}
			if spec.Required {
				return nil, fmt.Errorf("inputs.%s is required (--set %s=...)", name, name)
			}
			continue
		}
		if spec.Pattern != "" {
			str := fmt.Sprintf("%v", v)
			re, err := regexp.Compile(spec.Pattern)
			if err != nil {
				return nil, fmt.Errorf("inputs.%s: bad pattern: %w", name, err)
			}
			if !re.MatchString(str) {
				return nil, fmt.Errorf("inputs.%s: %q does not match %s", name, str, spec.Pattern)
			}
		}
		if len(spec.Values) > 0 {
			str := fmt.Sprintf("%v", v)
			found := false
			for _, allowed := range spec.Values {
				if str == allowed {
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("inputs.%s: %q not in %v", name, v, spec.Values)
			}
		}
		out[name] = v
	}
	for name := range given {
		if _, ok := wf.Inputs[name]; !ok {
			return nil, fmt.Errorf("unknown input %q", name)
		}
	}
	return out, nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func loadRunState(root, execID, digest string) (map[string]*StepState, error) {
	runDir := filepath.Join(root, execID)
	metaRaw, err := os.ReadFile(filepath.Join(runDir, "metadata.json"))
	if err != nil {
		return nil, fmt.Errorf("resume %s: %w", execID, err)
	}
	var meta struct {
		Digest string `json:"digest"`
	}
	_ = json.Unmarshal(metaRaw, &meta)
	if digest != "" && meta.Digest != "" && meta.Digest != digest {
		return nil, fmt.Errorf("resume %s: workflow digest changed (%s → %s) — a resumed run must execute the file it started with", execID, meta.Digest, digest)
	}
	states := map[string]*StepState{}
	entries, err := os.ReadDir(filepath.Join(runDir, "steps"))
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(runDir, "steps", e.Name()))
		if err != nil {
			continue
		}
		var st StepState
		if json.Unmarshal(raw, &st) == nil && st.Name != "" {
			states[st.Name] = &st
		}
	}
	return states, nil
}
