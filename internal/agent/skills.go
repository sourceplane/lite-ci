package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/remotestate"
)

// skills.go — the driver's playbooks (orun-initiatives-v2 IS6, design §8):
// `orun agent run`/`serve` fetch the hosted skill set and write each
// revision as a NATIVE skill file the harness discovers on its own
// (<dir>/<name>/SKILL.md — the Claude Code project-skill layout), beside
// the MCP config the runtime already writes. The session records the
// exact revisions it materialized (SkillPin); the IS6 PR manifest names
// them, so a review can always re-read the playbook the agent ran under.

// SkillPin names one skill revision a session ran under.
type SkillPin struct {
	Name string `json:"name"`
	Rev  string `json:"rev"`
}

// MaterializeSkills writes each skill as <dir>/<name>/SKILL.md — a
// frontmatter block (name, description when present, and the pinned
// orun-rev) over the canonical body — and returns the pins, name order.
func MaterializeSkills(dir string, skills []remotestate.SkillView) ([]SkillPin, error) {
	pins := make([]SkillPin, 0, len(skills))
	for _, s := range skills {
		skillDir := filepath.Join(dir, s.Name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return nil, fmt.Errorf("agent: skill dir %s: %w", s.Name, err)
		}
		var fm strings.Builder
		fm.WriteString("---\n")
		fm.WriteString("name: " + s.Name + "\n")
		if desc, ok := s.Frontmatter["description"].(string); ok && desc != "" {
			fm.WriteString("description: " + strings.ReplaceAll(desc, "\n", " ") + "\n")
		}
		// The pin travels IN the file too — a skill on disk always names
		// the revision it is, even away from the pins record.
		fm.WriteString("orun-rev: " + s.Rev + "\n")
		fm.WriteString("orun-source: " + s.Source + "\n")
		fm.WriteString("---\n\n")
		body := s.Body
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(fm.String()+body), 0o644); err != nil {
			return nil, fmt.Errorf("agent: skill %s: %w", s.Name, err)
		}
		pins = append(pins, SkillPin{Name: s.Name, Rev: s.Rev})
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].Name < pins[j].Name })
	return pins, nil
}

// WriteSkillPins records the materialized revisions beside the MCP config
// (skills.json) — the IS6 PR manifest's source of truth for what this
// session ran under.
func WriteSkillPins(path string, pins []SkillPin) error {
	b, err := json.MarshalIndent(map[string]any{"skills": pins}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("agent: skill pins dir: %w", err)
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}
