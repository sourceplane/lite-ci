package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractComplete pins the readiness predicate carried over from
// internal/worklens at WT2 — the definition of "actionable" the brief
// assembler reads.
func TestContractComplete(t *testing.T) {
	if (&Contract{}).Complete() {
		t.Fatal("an empty contract is not complete")
	}
	var nilC *Contract
	if nilC.Complete() {
		t.Fatal("a nil contract is not complete")
	}
	full := &Contract{Goal: "g", Affects: []string{"a/b/c"}, DoneWhen: []string{"green"}, Gates: []string{"tests"}}
	if !full.Complete() {
		t.Fatal("a full contract is complete")
	}
	emptyGates := &Contract{Goal: "g", Affects: []string{"a/b/c"}, DoneWhen: []string{"green"}, GatesDefined: true}
	if !emptyGates.Complete() {
		t.Fatal("an explicitly empty gate set is a declared gate set")
	}
	undeclared := &Contract{Goal: "g", Affects: []string{"a/b/c"}, DoneWhen: []string{"green"}}
	if undeclared.Complete() {
		t.Fatal("gates never declared is not complete")
	}
}

func briefFixture() Brief {
	return Brief{
		Spec: BriefSpec{Key: "demo-epic", Title: "Demo", DocRef: "sha256:doc"},
		Tasks: []BriefTask{
			{Key: "ORN-1", Title: "a"},
			{Key: "ORN-2", Title: "b", Contract: &Contract{Goal: "g", Affects: []string{"a/b/c"}, DoneWhen: []string{"d"}, Gates: []string{"tests"}}},
		},
		CoordSeq: 42,
		ObsSeq:   7,
	}
}

// TestSealDeterminism: the same logical content seals to the same id on
// every machine, and any change to the content shifts it.
func TestSealDeterminism(t *testing.T) {
	idA, bytesA, err := ContentID(briefFixture())
	if err != nil {
		t.Fatal(err)
	}
	idB, bytesB, err := ContentID(briefFixture())
	if err != nil {
		t.Fatal(err)
	}
	if idA != idB || string(bytesA) != string(bytesB) {
		t.Fatal("resealing identical inputs is not byte-identical")
	}
	if !strings.HasPrefix(idA, "sha256:") || len(idA) != 7+64 {
		t.Fatalf("content id shape: %s", idA)
	}

	moved := briefFixture()
	moved.CoordSeq = 43
	idMoved, _, err := ContentID(moved)
	if err != nil {
		t.Fatal(err)
	}
	if idMoved == idA {
		t.Fatal("changed input sealed to the same id")
	}
}

// TestIDForBytesIgnoresFormatting: identity is content, not layout — a
// pretty-printed brief and its compact twin carry the same id.
func TestIDForBytesIgnoresFormatting(t *testing.T) {
	_, canonical, err := ContentID(briefFixture())
	if err != nil {
		t.Fatal(err)
	}
	var tree interface{}
	if err := json.Unmarshal(canonical, &tree); err != nil {
		t.Fatal(err)
	}
	pretty, err := json.MarshalIndent(tree, "", "    ")
	if err != nil {
		t.Fatal(err)
	}

	idCompact, err := IDForBytes(canonical)
	if err != nil {
		t.Fatal(err)
	}
	idPretty, err := IDForBytes(pretty)
	if err != nil {
		t.Fatal(err)
	}
	if idCompact != idPretty {
		t.Fatalf("formatting shifted the id: %s vs %s", idCompact, idPretty)
	}
	if err := VerifySealedBytes(idCompact, pretty); err != nil {
		t.Fatalf("verify against the re-formatted bytes: %v", err)
	}
	if err := VerifySealedBytes("sha256:"+strings.Repeat("0", 64), canonical); err == nil {
		t.Fatal("verify accepted a wrong id")
	}
}

// TestSealRefusesDerivedState: a brief is intent only. A payload carrying
// fold output is refused at the seal, not silently frozen.
func TestSealRefusesDerivedState(t *testing.T) {
	raw := []byte(`{"spec":{"key":"demo"},"tasks":[{"key":"ORN-1","rung":"in_review"}],"coordSeq":1,"obsSeq":1}`)
	if _, err := IDForBytes(raw); err == nil || !strings.Contains(err.Error(), "derived state") {
		t.Fatalf("sealing a brief carrying a rung must fail, got %v", err)
	}
}

// TestDecodeBrief: the decode is lenient about fields it does not model,
// but the id it returns is over the bytes on disk — so a lossy decode can
// never fabricate a matching id.
func TestDecodeBrief(t *testing.T) {
	_, canonical, err := ContentID(briefFixture())
	if err != nil {
		t.Fatal(err)
	}
	b, id, err := DecodeBrief(canonical)
	if err != nil {
		t.Fatal(err)
	}
	want, _, _ := ContentID(briefFixture())
	if id != want {
		t.Fatalf("decoded id %s, want %s", id, want)
	}
	task, ok := b.Task("ORN-2")
	if !ok || task.Contract == nil || task.Contract.Goal != "g" {
		t.Fatalf("task contract did not survive the decode: %+v", task)
	}
	if _, ok := b.Task("ORN-404"); ok {
		t.Fatal("an unknown task key resolved")
	}

	withExtra := []byte(`{"apiVersion":"orun.io/v1","kind":"SpecSnapshot","spec":{"key":"demo"},"tasks":[],"coordSeq":1,"obsSeq":2}`)
	decoded, gotID, err := DecodeBrief(withExtra)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Spec.Key != "demo" || decoded.ObsSeq != 2 {
		t.Fatalf("lenient decode dropped modelled fields: %+v", decoded)
	}
	if _, _, err := ContentID(*decoded); err != nil {
		t.Fatal(err)
	}
	reencoded, _, _ := ContentID(*decoded)
	if reencoded == gotID {
		t.Fatal("re-encoding a lossy decode must not reproduce the on-disk id")
	}
	if _, _, err := DecodeBrief([]byte("not json")); err == nil {
		t.Fatal("decoding garbage must fail")
	}
}
