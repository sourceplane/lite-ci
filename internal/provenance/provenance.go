// Package provenance is the pen (orun-initiatives-v2 IS6, design §9): every
// PR carries its lineage — the branch names the task, the body carries the
// machine-readable manifest, commits carry the Orun-Task trailer — and the
// cloud's `orun/compliance` check-run (IS7) verifies what the pen wrote.
// Provenance is not bureaucracy: it is how the observation log knows which
// task a branch, PR, or check belongs to.
//
// This package is pure rules + rendering; the side-effectful half (branch,
// push, the GitHub API call) lives in pen.go. IS7 pins `orun pr check` and
// the cloud evaluator byte-identical on shared fixtures; Verify's findings
// are the seam that engine grows from.
package provenance

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// BranchRe is the branch grammar: orun/<task-key>-<slug>. The task key in
// the branch name is how branch_seen observations claim the task — one
// task, one branch. Key forms: typed (PAY-T14), legacy (ORN-142), triage
// (WRK-9).
var BranchRe = regexp.MustCompile(`^orun/([A-Z][A-Z0-9]{1,5}-(?:[A-Z]?)[0-9]+)(?:-([a-z0-9-]+))?$`)

// BranchName renders the grammar for a task key and a slug.
func BranchName(taskKey, slug string) string {
	slug = Slugify(slug)
	if slug == "" {
		return "orun/" + taskKey
	}
	return "orun/" + taskKey + "-" + slug
}

// TaskKeyOfBranch extracts the task key a branch names, or "".
func TaskKeyOfBranch(branch string) string {
	m := BranchRe.FindStringSubmatch(branch)
	if m == nil {
		return ""
	}
	return m[1]
}

// Slugify lowers a title into the branch slug alphabet.
func Slugify(s string) string {
	var b strings.Builder
	lastHyphen := true
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 48 {
		out = strings.TrimRight(out[:48], "-")
	}
	return out
}

// ── The manifest (design §9.1): the PR body's machine-readable block ────────

// ManifestVersion pins the block's schema; the compliance evaluator (IS7)
// refuses versions it does not know rather than guessing.
const ManifestVersion = 1

// SkillPin names one skill revision the session ran under.
type SkillPin struct {
	Name string `json:"name"`
	Rev  string `json:"rev"`
}

// Manifest is the lineage block: the task this PR closes, the epic and the
// approved revision it was dispatched under, the skill revisions the
// session ran with, and the session itself.
type Manifest struct {
	Version      int        `json:"version"`
	Task         string     `json:"task"`
	Epic         string     `json:"epic,omitempty"`
	EpicRevision string     `json:"epicRevision,omitempty"`
	Skills       []SkillPin `json:"skills,omitempty"`
	Session      string     `json:"session,omitempty"`
}

const manifestOpen = "<!-- orun:manifest"
const manifestClose = "-->"

// RenderManifest renders the block the PR body carries. An HTML comment on
// purpose: machine-readable, verbatim-preserved by GitHub, invisible in the
// rendered body — the lineage rides along without shouting.
func RenderManifest(m Manifest) (string, error) {
	if m.Version == 0 {
		m.Version = ManifestVersion
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	return manifestOpen + "\n" + string(b) + "\n" + manifestClose, nil
}

// ParseManifest extracts and decodes the block from a PR body; nil when no
// block is present, error when a block is present but malformed (a broken
// manifest is a finding, not a silent absence).
func ParseManifest(body string) (*Manifest, error) {
	start := strings.Index(body, manifestOpen)
	if start < 0 {
		return nil, nil
	}
	rest := body[start+len(manifestOpen):]
	end := strings.Index(rest, manifestClose)
	if end < 0 {
		return nil, fmt.Errorf("provenance: manifest block is unterminated")
	}
	var m Manifest
	if err := json.Unmarshal([]byte(strings.TrimSpace(rest[:end])), &m); err != nil {
		return nil, fmt.Errorf("provenance: manifest block is not valid JSON: %w", err)
	}
	return &m, nil
}

// ── The trailer (design §9.1): commits name their task ──────────────────────

const trailerKey = "Orun-Task:"

// TaskTrailer renders the commit trailer line.
func TaskTrailer(taskKey string) string { return trailerKey + " " + taskKey }

// EnsureTrailer appends the trailer to a commit message when absent —
// idempotent, so the commit-msg hook can run on amends without stacking.
func EnsureTrailer(message, taskKey string) string {
	if TrailerTaskKey(message) != "" {
		return message
	}
	msg := strings.TrimRight(message, "\n")
	return msg + "\n\n" + TaskTrailer(taskKey) + "\n"
}

// TrailerTaskKey extracts the trailer's task key from a commit message, "".
func TrailerTaskKey(message string) string {
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, trailerKey) {
			return strings.TrimSpace(strings.TrimPrefix(line, trailerKey))
		}
	}
	return ""
}

// ── The preflight (design §9.4: prevention over detection) ──────────────────

// Finding is one preflight verdict. Level "error" fails `orun pr check`;
// "warn" prints and passes. The same shape the IS7 shared rule engine
// grows from — the cloud evaluator must return byte-identical verdicts on
// shared fixtures.
type Finding struct {
	Level string `json:"level"` // error | warn
	Rule  string `json:"rule"`
	Text  string `json:"text"`
}

// CheckInput is everything the local preflight can see without a network.
type CheckInput struct {
	Branch         string
	TaskKey        string   // expected task; "" = derive from branch
	CommitMessages []string // commits ahead of the base
	Manifest       *Manifest
	HasSkillPins   bool // .orun/agent-mcp/skills.json existed for the session
}

// Verify runs the v1 rule set: the branch names a task, everything that
// names a task agrees on WHICH task (one task, one PR), commits carry the
// trailer, and the manifest — when present — is coherent.
func Verify(in CheckInput) []Finding {
	var out []Finding
	branchKey := TaskKeyOfBranch(in.Branch)
	if branchKey == "" {
		out = append(out, Finding{Level: "error", Rule: "branch-grammar",
			Text: fmt.Sprintf("branch %q does not match orun/<task-key>-<slug>", in.Branch)})
	}
	want := in.TaskKey
	if want == "" {
		want = branchKey
	}
	if want != "" && branchKey != "" && branchKey != want {
		out = append(out, Finding{Level: "error", Rule: "one-task-one-pr",
			Text: fmt.Sprintf("branch names %s but the PR is for %s", branchKey, want)})
	}
	for _, msg := range in.CommitMessages {
		got := TrailerTaskKey(msg)
		first := strings.SplitN(msg, "\n", 2)[0]
		if got == "" {
			out = append(out, Finding{Level: "error", Rule: "task-trailer",
				Text: fmt.Sprintf("commit %q lacks the Orun-Task trailer (orun githooks install adds it)", truncate(first))})
		} else if want != "" && got != want {
			out = append(out, Finding{Level: "error", Rule: "one-task-one-pr",
				Text: fmt.Sprintf("commit %q is trailed for %s, not %s", truncate(first), got, want)})
		}
	}
	if in.Manifest == nil {
		out = append(out, Finding{Level: "warn", Rule: "manifest",
			Text: "no manifest block — `orun pr open` writes it; the compliance check will flag its absence"})
	} else {
		if in.Manifest.Version != ManifestVersion {
			out = append(out, Finding{Level: "error", Rule: "manifest",
				Text: fmt.Sprintf("manifest version %d is not %d", in.Manifest.Version, ManifestVersion)})
		}
		if want != "" && in.Manifest.Task != want {
			out = append(out, Finding{Level: "error", Rule: "one-task-one-pr",
				Text: fmt.Sprintf("manifest names %s, not %s", in.Manifest.Task, want)})
		}
		if in.HasSkillPins && len(in.Manifest.Skills) == 0 {
			out = append(out, Finding{Level: "warn", Rule: "skill-pins",
				Text: "the session recorded skill pins but the manifest names none"})
		}
	}
	return out
}

// HasErrors reports whether any finding fails the preflight.
func HasErrors(findings []Finding) bool {
	for _, f := range findings {
		if f.Level == "error" {
			return true
		}
	}
	return false
}

func truncate(s string) string {
	if len(s) > 60 {
		return s[:57] + "…"
	}
	return s
}
