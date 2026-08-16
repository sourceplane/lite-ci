package penmcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/mcpserve"
	"github.com/sourceplane/orun/internal/provenance"
)

// fakePen records pr_open gestures and answers like the honest pen.
type fakePen struct {
	opened []provenance.OpenRequest
	url    string // "" = anonymous (compare-URL fallback)
}

func (f *fakePen) Open(_ context.Context, req provenance.OpenRequest) (*provenance.OpenResult, error) {
	f.opened = append(f.opened, req)
	out := &provenance.OpenResult{Branch: "orun/" + req.TaskKey + "-x", Pushed: true, Body: "body"}
	if f.url != "" {
		out.Opened = true
		out.URL = f.url
	} else {
		out.CompareURL = "https://github.com/o/r/compare/main...orun/" + req.TaskKey + "-x?expand=1"
	}
	return out, nil
}

func call(t *testing.T, p *Provider, args string) (string, bool, bool) {
	t.Helper()
	res, owned := p.Call(context.Background(), ToolName, json.RawMessage(args))
	if !owned {
		return "", false, false
	}
	blocks, _ := res["content"].([]map[string]interface{})
	if len(blocks) != 1 {
		t.Fatalf("result content = %+v", res)
	}
	text, _ := blocks[0]["text"].(string)
	isErr, _ := res["isError"].(bool)
	return text, isErr, true
}

// TestPrOpen: mounted, the pen opens (or honestly prepares) the task's PR;
// unmounted, the verdict says exactly what to do instead. task stays
// required — a PR opens FOR a task.
func TestPrOpen(t *testing.T) {
	pen := &fakePen{url: "https://github.com/o/r/pull/7"}
	p := &Provider{Pen: pen}

	text, isErr, _ := call(t, p, `{"task":"PAY-T14","title":"Route the reads","draft":true}`)
	if isErr || !strings.Contains(text, "opened https://github.com/o/r/pull/7") {
		t.Fatalf("pr_open result: %s", text)
	}
	if len(pen.opened) != 1 || pen.opened[0].TaskKey != "PAY-T14" || !pen.opened[0].Draft {
		t.Fatalf("pen gestures = %+v", pen.opened)
	}
	if text, isErr, _ = call(t, p, `{}`); !isErr || !strings.Contains(text, "task is required") {
		t.Fatalf("pr_open without a task must fail: %s", text)
	}

	// Anonymous pen: the compare URL comes back — prepared, never faked.
	anon := &Provider{Pen: &fakePen{}}
	if text, isErr, _ = call(t, anon, `{"task":"PAY-T14"}`); isErr || !strings.Contains(text, "compare/main...orun/PAY-T14") {
		t.Fatalf("anonymous pr_open result: %s", text)
	}

	// No pen mounted (a repo-less serve): a clear verdict, not a git guess.
	bare := &Provider{}
	if text, isErr, _ = call(t, bare, `{"task":"PAY-T14"}`); !isErr || !strings.Contains(text, "no repository workspace mounted") {
		t.Fatalf("penless pr_open verdict: %s", text)
	}
}

// TestOwnership: the plane disowns every name but its own, so another
// provider answers it rather than this one claiming an unknown tool.
func TestOwnership(t *testing.T) {
	p := &Provider{Pen: &fakePen{}}
	if _, owned := p.Call(context.Background(), "catalog_search", json.RawMessage(`{}`)); owned {
		t.Fatal("penmcp claimed a tool it does not own")
	}
	if names := ToolNames(); len(names) != 1 || names[0] != ToolName {
		t.Fatalf("roster = %v, want exactly [%s]", names, ToolName)
	}
}

// TestAnnotationsComplete: every composed tool must carry all three wire
// hints (UM4) — an absent hint reads as "unknown, treat as a write".
func TestAnnotationsComplete(t *testing.T) {
	for _, tool := range Tools() {
		for _, hint := range mcpserve.AnnotationHints {
			if _, ok := tool.Annotations[hint].(bool); !ok {
				t.Errorf("%s: missing %s annotation", tool.Name, hint)
			}
		}
		if ro, _ := tool.Annotations["readOnlyHint"].(bool); ro {
			t.Errorf("%s: the pen writes — readOnlyHint must be false", tool.Name)
		}
	}
}
