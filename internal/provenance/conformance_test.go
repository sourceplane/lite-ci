package provenance

import (
	_ "embed"
	"encoding/json"
	"testing"
)

//go:embed fixtures/provenance-conformance.json
var conformanceJSON []byte

// The shared rule-engine contract (orun-initiatives-v2 IS7, design §9 /
// IS-G): fixtures/provenance-conformance.json is the byte-identical twin of
// orun-cloud packages/db/src/work/fixtures/provenance-conformance.json.
// `orun pr check` (this engine) and the cloud orun/compliance evaluator
// replay the same cases and must emit exactly these findings — same order,
// same texts. Change the file only in lockstep with BOTH engines.
type provenanceConformance struct {
	Version int `json:"version"`
	Cases   []struct {
		Name  string `json:"name"`
		Input struct {
			Branch         string    `json:"branch"`
			TaskKey        string    `json:"taskKey"`
			CommitMessages []string  `json:"commitMessages"`
			Manifest       *Manifest `json:"manifest"`
			HasSkillPins   bool      `json:"hasSkillPins"`
		} `json:"input"`
		Findings []Finding `json:"findings"`
	} `json:"cases"`
}

func TestProvenanceConformance(t *testing.T) {
	var fx provenanceConformance
	if err := json.Unmarshal(conformanceJSON, &fx); err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	if fx.Version != 1 {
		t.Fatalf("fixture version = %d, want 1", fx.Version)
	}
	if len(fx.Cases) == 0 {
		t.Fatal("no fixture cases")
	}
	for _, tc := range fx.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			got := Verify(CheckInput{
				Branch:         tc.Input.Branch,
				TaskKey:        tc.Input.TaskKey,
				CommitMessages: tc.Input.CommitMessages,
				Manifest:       tc.Input.Manifest,
				HasSkillPins:   tc.Input.HasSkillPins,
			})
			if len(got) != len(tc.Findings) {
				t.Fatalf("findings = %+v, want %+v", got, tc.Findings)
			}
			for i, want := range tc.Findings {
				if got[i] != want {
					t.Errorf("finding[%d] = %+v, want %+v", i, got[i], want)
				}
			}
		})
	}
}
