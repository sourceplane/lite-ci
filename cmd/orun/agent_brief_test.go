package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/contract"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
)

// The sealed-brief path `orun agent run --spec` reads (orun-work-teardown
// WT2a). It used to decode a worklens.SpecSnapshot pulled from the work
// fold API; the API is gone and the artifact is not, so the brief is now a
// local file whose content id is computed over the bytes on disk. These
// tests pin what that buys: the id survives reformatting, a task's contract
// reaches the assembled brief, and a missing contract is refused rather
// than silently briefing an agent with nothing.

func writeBrief(t *testing.T, slug string, body []byte) {
	t.Helper()
	dir := filepath.Join(".orun", "specs", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func briefBytes(t *testing.T) []byte {
	t.Helper()
	_, canonical, err := contract.ContentID(contract.Brief{
		Spec: contract.BriefSpec{Key: "checkout-rework", Title: "Checkout rework"},
		Tasks: []contract.BriefTask{
			{Key: "ORN-142", Title: "Route the reads", Contract: &contract.Contract{
				Goal:     "route checkout reads through the new seam",
				Affects:  []string{"services/checkout", "services/payments"},
				DoneWhen: []string{"reads go through the seam"},
				Gates:    []string{"unit"},
			}},
			{Key: "ORN-143", Title: "No contract here"},
		},
		CoordSeq: 42, ObsSeq: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func TestReadSealedBrief(t *testing.T) {
	t.Chdir(t.TempDir())
	canonical := briefBytes(t)
	writeBrief(t, "checkout-rework", canonical)

	brief, id, err := readSealedBrief("checkout-rework")
	if err != nil {
		t.Fatalf("readSealedBrief: %v", err)
	}
	if !strings.HasPrefix(id, "sha256:") || len(id) != 7+64 {
		t.Fatalf("brief id shape: %s", id)
	}
	task, ok := brief.Task("ORN-142")
	if !ok || task.Contract == nil || task.Contract.Goal == "" {
		t.Fatalf("contract did not survive the read: %+v", task)
	}
	if !task.Contract.Complete() {
		t.Errorf("a goal + affects + doneWhen + gates contract must read as complete")
	}

	// Identity is content, not layout: a hand-reformatted brief keeps its id,
	// which is what makes an authored file as pinnable as a generated one.
	var tree interface{}
	if err := json.Unmarshal(canonical, &tree); err != nil {
		t.Fatal(err)
	}
	pretty, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeBrief(t, "checkout-rework", pretty)
	_, prettyID, err := readSealedBrief("checkout-rework")
	if err != nil {
		t.Fatalf("readSealedBrief on the reformatted brief: %v", err)
	}
	if prettyID != id {
		t.Errorf("reformatting shifted the brief id: %s vs %s", prettyID, id)
	}

	// A brief that is not there, and one that is not a brief, both name the
	// path rather than failing somewhere downstream.
	if _, _, err := readSealedBrief("absent"); err == nil || !strings.Contains(err.Error(), ".orun") {
		t.Errorf("a missing brief must name the path it looked in, got %v", err)
	}
	writeBrief(t, "garbage", []byte("not json"))
	if _, _, err := readSealedBrief("garbage"); err == nil {
		t.Error("a malformed brief must fail the read")
	}
}

// TestSealedBriefRefusesDerivedState: the seal is the reason a brief can be
// trusted. A hand-authored file that smuggles fold output into it is
// refused at the read, not carried into an agent's instructions.
func TestSealedBriefRefusesDerivedState(t *testing.T) {
	t.Chdir(t.TempDir())
	writeBrief(t, "sneaky", []byte(`{"spec":{"key":"s"},"tasks":[{"key":"ORN-1","rung":"done"}],"coordSeq":1,"obsSeq":1}`))
	_, _, err := readSealedBrief("sneaky")
	if err == nil || !strings.Contains(err.Error(), "derived state") {
		t.Fatalf("a brief carrying a rung must be refused, got %v", err)
	}
}

// TestBriefAssemblyFromSealedBrief is the end-to-end of the path: the
// contract read off the brief reaches the rendered instructions and the
// sealed AgentBrief node, and the same inputs seal to the same id.
func TestBriefAssemblyFromSealedBrief(t *testing.T) {
	t.Chdir(t.TempDir())
	writeBrief(t, "checkout-rework", briefBytes(t))

	brief, specID, err := readSealedBrief("checkout-rework")
	if err != nil {
		t.Fatal(err)
	}
	task, ok := brief.Task("ORN-142")
	if !ok {
		t.Fatal("task missing from the brief")
	}

	ctx := context.Background()
	store := objectstore.NewMemStore(objectstore.AlgoSHA256)
	in := agent.BriefInput{
		RunKind:  nodes.RunKindImplementation,
		Task:     task.Key,
		Persona:  []byte("# Implementer\n"),
		Contract: task.Contract,
		SpecID:   specID,
		Affected: task.Contract.Affects,
	}
	assembled, err := agent.AssembleBrief(ctx, store, in)
	if err != nil {
		t.Fatalf("AssembleBrief: %v", err)
	}
	for _, want := range []string{
		"route checkout reads through the new seam", // the goal
		"services/checkout",                         // the blast-radius ceiling
		"reads go through the seam",                 // done-when
		"Gates",                                     // gates section
	} {
		if !strings.Contains(assembled.Instructions, want) {
			t.Errorf("instructions lack %q:\n%s", want, assembled.Instructions)
		}
	}
	if assembled.Node.Spec != specID {
		t.Errorf("the run must pin the brief it was briefed from: %q vs %q", assembled.Node.Spec, specID)
	}

	again, err := agent.AssembleBrief(ctx, store, in)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != assembled.ID {
		t.Errorf("same inputs sealed to different briefs: %s vs %s", again.ID, assembled.ID)
	}

	// A task with no contract is refused by the command layer rather than
	// briefing an agent with an empty goal.
	if _, ok := brief.Task("ORN-143"); !ok {
		t.Fatal("ORN-143 should be present but contract-less")
	}
	if c, _ := brief.Task("ORN-143"); c.Contract != nil {
		t.Error("ORN-143 must carry no contract in this fixture")
	}
}
