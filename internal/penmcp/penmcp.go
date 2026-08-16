// Package penmcp serves the provenance pen over MCP: the pr_open tool that
// opens a task's PR with its lineage written into the branch name and the
// body's machine-readable manifest.
//
// It was carved out of internal/workmcp at the work-plane teardown
// (orun-work-teardown WT2). pr_open never touched the work plane — it
// drives internal/provenance against the checkout, exactly as `orun pr
// open` does, and the cloud's orun/compliance check verifies what it
// wrote. It outlives the tracker, so it moves out before the tracker is
// deleted rather than dying with it.
package penmcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sourceplane/orun/internal/mcpserve"
	"github.com/sourceplane/orun/internal/provenance"
)

// ToolName is the single tool this plane owns.
const ToolName = "pr_open"

// Pen is the side-effectful seam: mounted when the serve runs inside a
// repository workspace, nil otherwise (the tool then answers with a clear
// verdict instead of guessing at git).
type Pen interface {
	Open(ctx context.Context, req provenance.OpenRequest) (*provenance.OpenResult, error)
}

// Provider is the pen's mcpserve.ToolProvider.
type Provider struct {
	Pen Pen
}

// Tools returns the plane's roster — one write tool.
func Tools() []mcpserve.ToolDef {
	return []mcpserve.ToolDef{{
		Name:        ToolName,
		Description: "Open the task's PR with its lineage written by the pen: the branch renamed onto the grammar (orun/<task-key>-<slug>) when needed, pushed, and the machine-readable manifest block in the body — the task, the skill revisions this session ran under, the session id. With a GitHub credential ambient the PR opens via the API; without one the pen prepares everything and returns the compare URL plus the body to use — honest either way. One task, one PR.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task":  map[string]interface{}{"type": "string", "description": "the task this PR closes"},
				"title": map[string]interface{}{"type": "string", "description": "PR title (optional; default the task key)"},
				"base":  map[string]interface{}{"type": "string", "description": "base branch (optional; default main)"},
				"draft": map[string]interface{}{"type": "boolean", "description": "open as a draft (optional)"},
			},
			"required": []string{"task"},
		},
		Annotations: mcpserve.Annotations(false, false, false),
	}}
}

// ToolNames returns the plane's tool names in definition order.
func ToolNames() []string {
	defs := Tools()
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
}

// Tools implements mcpserve.ToolProvider.
func (p *Provider) Tools() []mcpserve.ToolDef { return Tools() }

// Call implements mcpserve.ToolProvider: owned=false for names outside this
// plane, and every owned failure maps to an isError result — the pen's
// verdict is something the agent should reason about, not a protocol fault.
func (p *Provider) Call(ctx context.Context, name string, args json.RawMessage) (mcpserve.Result, bool) {
	if name != ToolName {
		return nil, false
	}
	text, err := p.open(ctx, args)
	if err != nil {
		return mcpserve.TextResult(fmt.Sprintf("error: %v", err), true), true
	}
	return mcpserve.TextResult(text, false), true
}

func (p *Provider) open(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Task  string `json:"task"`
		Title string `json:"title"`
		Base  string `json:"base"`
		Draft bool   `json:"draft"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Task == "" {
		return "", fmt.Errorf("pr_open: task is required — a PR opens FOR a task")
	}
	if p.Pen == nil {
		return "", fmt.Errorf("pr_open: no repository workspace mounted in this serve — run `orun pr open --task %s` from the checkout instead", a.Task)
	}
	out, err := p.Pen.Open(ctx, provenance.OpenRequest{TaskKey: a.Task, Title: a.Title, Base: a.Base, Draft: a.Draft})
	if err != nil {
		return "", err
	}
	if out.Opened {
		return fmt.Sprintf("opened %s (branch %s) — the manifest rides the body", out.URL, out.Branch), nil
	}
	return fmt.Sprintf("branch %s pushed; no GitHub credential ambient — open it here: %s\n\nbody to use:\n%s", out.Branch, out.CompareURL, out.Body), nil
}
