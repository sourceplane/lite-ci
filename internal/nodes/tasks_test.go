package nodes

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

func taskRecordFixture() TaskRecord {
	return TaskRecord{
		Kind:         KindTask,
		APIVersion:   apiVersionV1,
		Key:          "ENG-42",
		TaskRef:      "tsk_3KF9TQ2P",
		Title:        "Ship the composer",
		ContractHash: "sha256:" + repeatHex("ab", 32),
	}
}

func repeatHex(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

func TestTaskRecordValidate(t *testing.T) {
	t.Parallel()
	ok := taskRecordFixture()
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid record refused: %v", err)
	}

	cases := map[string]func(*TaskRecord){
		"wrong kind":        func(r *TaskRecord) { r.Kind = "Job" },
		"wrong api version": func(r *TaskRecord) { r.APIVersion = "orun.io/v2" },
		"lowercase key":     func(r *TaskRecord) { r.Key = "eng-42" },
		"digit-led key":     func(r *TaskRecord) { r.Key = "9LIVES-1" },
		"empty key":         func(r *TaskRecord) { r.Key = "" },
		"malformed taskRef": func(r *TaskRecord) { r.TaskRef = "tsk_lower" },
		"malformed seal":    func(r *TaskRecord) { r.ContractHash = "sha256:xyz" },
	}
	for name, mutate := range cases {
		rec := taskRecordFixture()
		mutate(&rec)
		if err := rec.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// Optional fields are genuinely optional: a just-created task has no
	// tsk_ ref yet (sync pending) and no contract (no narrowing, TK-4).
	bare := TaskRecord{Kind: KindTask, APIVersion: apiVersionV1, Key: "WEB-1"}
	if err := bare.Validate(); err != nil {
		t.Fatalf("bare record refused: %v", err)
	}
}

func TestAssembleTaskBundlesContractInTree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := objectstore.NewMemStore(objectstore.AlgoSHA256)

	wire := []byte(`{"goal":"ship it"}`)
	id, err := AssembleTask(ctx, s, taskRecordFixture(), wire)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	entries, err := s.GetTree(ctx, id)
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	names := map[string]objectstore.ObjectID{}
	for _, e := range entries {
		names[e.Name] = e.ID
	}
	if _, ok := names["task.json"]; !ok {
		t.Fatal("tree carries no task.json")
	}
	// The contract rides IN the tree — the reachability property GC depends
	// on (a string-field reference would not be walked).
	contractID, ok := names["contract.json"]
	if !ok {
		t.Fatal("tree carries no contract.json")
	}
	_, body, err := s.Get(ctx, contractID)
	if err != nil {
		t.Fatalf("contract blob: %v", err)
	}
	if string(body) != string(wire) {
		t.Fatalf("contract bytes changed in the store: %s", body)
	}
}

func TestAssembleTaskRefusesSealBytesMismatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := objectstore.NewMemStore(objectstore.AlgoSHA256)

	sealNoBytes := taskRecordFixture()
	if _, err := AssembleTask(ctx, s, sealNoBytes, nil); err == nil {
		t.Fatal("a seal without contract bytes was assembled")
	}

	bytesNoSeal := taskRecordFixture()
	bytesNoSeal.ContractHash = ""
	if _, err := AssembleTask(ctx, s, bytesNoSeal, []byte("{}")); err == nil {
		t.Fatal("contract bytes without a seal were assembled")
	}
}

func TestTaskIDMatchesAssembleTask(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := objectstore.NewMemStore(objectstore.AlgoSHA256)
	wire := []byte(`{"goal":"g"}`)

	pure, err := TaskID(objectstore.AlgoSHA256, taskRecordFixture(), wire)
	if err != nil {
		t.Fatalf("TaskID: %v", err)
	}
	written, err := AssembleTask(ctx, s, taskRecordFixture(), wire)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if pure != written {
		t.Fatalf("pure id %s != assembled id %s", pure, written)
	}
}

func TestAssembleTaskSurfacesStoreFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wire := []byte(`{"goal":"g"}`)

	// The coverage_test failStore, reused: fail the record blob, then the
	// contract blob, then the tree — the branches a healthy store never takes.
	if _, err := AssembleTask(ctx, newFail(1, 0), taskRecordFixture(), wire); err == nil {
		t.Fatal("record-blob failure swallowed")
	}
	if _, err := AssembleTask(ctx, newFail(2, 0), taskRecordFixture(), wire); err == nil {
		t.Fatal("contract-blob failure swallowed")
	}
	if _, err := AssembleTask(ctx, newFail(0, 1), taskRecordFixture(), wire); err == nil {
		t.Fatal("tree failure swallowed")
	}
}
