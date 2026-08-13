package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

// IS6: materialization writes NATIVE skill files — the harness discovers
// them on its own — with the pinned revision IN the file, and the pins
// record what this session ran under (the PR manifest's source).

func skillView(name, rev, desc, body string) remotestate.SkillView {
	v := remotestate.SkillView{Body: body}
	v.Name = name
	v.Rev = rev
	v.Source = "default"
	v.Frontmatter = map[string]interface{}{"description": desc}
	return v
}

func TestMaterializeSkills(t *testing.T) {
	dir := t.TempDir()
	pins, err := MaterializeSkills(dir, []remotestate.SkillView{
		skillView("pr-provenance", "sha256:bb", "The pen.", "# PR provenance\n\nBranch grammar."),
		skillView("milestone-loop", "sha256:aa", "One milestone in flight.", "# The milestone loop\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Pins come back name-ordered — a stable manifest, whatever the fetch order.
	if len(pins) != 2 || pins[0].Name != "milestone-loop" || pins[1].Rev != "sha256:bb" {
		t.Fatalf("pins = %+v", pins)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "milestone-loop", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"name: milestone-loop",
		"description: One milestone in flight.",
		"orun-rev: sha256:aa", // the pin travels IN the file
		"# The milestone loop",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("SKILL.md lacks %q:\n%s", want, text)
		}
	}
	if !strings.HasPrefix(text, "---\n") {
		t.Error("SKILL.md must open with the frontmatter block")
	}
	// A body without a trailing newline gains one — file hygiene.
	prBody, _ := os.ReadFile(filepath.Join(dir, "pr-provenance", "SKILL.md"))
	if !strings.HasSuffix(string(prBody), "\n") {
		t.Error("materialized body must end with a newline")
	}

	pinsPath := filepath.Join(t.TempDir(), "agent-mcp", "skills.json")
	if err := WriteSkillPins(pinsPath, pins); err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Skills []SkillPin `json:"skills"`
	}
	b, _ := os.ReadFile(pinsPath)
	if err := json.Unmarshal(b, &recorded); err != nil {
		t.Fatal(err)
	}
	if len(recorded.Skills) != 2 || recorded.Skills[0].Rev != "sha256:aa" {
		t.Fatalf("recorded pins = %+v", recorded.Skills)
	}
}
