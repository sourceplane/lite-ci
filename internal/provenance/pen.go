package provenance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// pen.go — the side-effectful half: the branch, the push, the PR. Every
// dependency is injectable (git, the token, the HTTP client), so the rules
// stay testable without a repository or a network. The pen is HONEST about
// what it could do: when no GitHub credential is ambient it still prepares
// everything (branch pushed, manifest rendered) and hands back the compare
// URL instead of pretending.

// githubRemoteRe mirrors cliauth's grammar: a github.com remote in ssh or
// https form.
var githubRemoteRe = regexp.MustCompile(`github\.com[:/]+([^/]+)/([^/]+?)(?:\.git)?$`)

// Pen executes the provenance-writing gestures in one workdir.
type Pen struct {
	Workdir string
	// RunGit executes git with the given args in Workdir. Default: exec git.
	RunGit func(ctx context.Context, args ...string) (string, error)
	// Token resolves a GitHub credential; empty = anonymous (prepare-only).
	Token func() string
	// HTTP performs the API call. Default: a 15s-timeout client.
	HTTP *http.Client
	// APIBase overrides https://api.github.com (tests).
	APIBase string
}

func (p *Pen) git(ctx context.Context, args ...string) (string, error) {
	if p.RunGit != nil {
		return p.RunGit(ctx, args...)
	}
	full := append([]string{"-C", p.Workdir}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// OpenRequest is one pr-open gesture.
type OpenRequest struct {
	TaskKey string
	Title   string
	Base    string // default: the remote's default branch guess ("main")
	Draft   bool
	// Prose is the human half of the body; the manifest block is appended.
	Prose    string
	Manifest Manifest
}

// OpenResult reports what the pen actually did.
type OpenResult struct {
	Branch     string `json:"branch"`
	Pushed     bool   `json:"pushed"`
	Opened     bool   `json:"opened"`
	URL        string `json:"url,omitempty"`        // the PR when opened
	CompareURL string `json:"compareUrl,omitempty"` // the fallback gesture
	Body       string `json:"body"`                 // prose + manifest, as sent (or to paste)
}

// Open ensures the grammar branch, pushes it, and opens the PR when a
// credential is ambient — otherwise prepares everything and returns the
// compare URL. The manifest block always rides the body.
func (p *Pen) Open(ctx context.Context, req OpenRequest) (*OpenResult, error) {
	if req.TaskKey == "" {
		return nil, fmt.Errorf("provenance: a PR opens FOR a task — task key required")
	}
	base := req.Base
	if base == "" {
		base = "main"
	}

	current, err := p.git(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, err
	}
	branch := current
	if TaskKeyOfBranch(current) != req.TaskKey {
		branch = BranchName(req.TaskKey, req.Title)
		if _, err := p.git(ctx, "checkout", "-B", branch); err != nil {
			return nil, err
		}
	}

	if _, err := p.git(ctx, "push", "-u", "origin", branch); err != nil {
		return nil, err
	}

	remote, err := p.git(ctx, "remote", "get-url", "origin")
	if err != nil {
		return nil, err
	}
	m := githubRemoteRe.FindStringSubmatch(remote)
	if m == nil {
		return nil, fmt.Errorf("provenance: origin %q is not a github.com repository", remote)
	}
	owner, repo := m[1], m[2]

	req.Manifest.Task = req.TaskKey
	block, err := RenderManifest(req.Manifest)
	if err != nil {
		return nil, err
	}
	body := strings.TrimRight(req.Prose, "\n")
	if body != "" {
		body += "\n\n"
	}
	body += block + "\n"

	out := &OpenResult{Branch: branch, Pushed: true, Body: body}

	token := ""
	if p.Token != nil {
		token = p.Token()
	}
	if token == "" {
		out.CompareURL = fmt.Sprintf("https://github.com/%s/%s/compare/%s...%s?expand=1", owner, repo, base, branch)
		return out, nil
	}

	title := req.Title
	if title == "" {
		title = req.TaskKey
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"title": title, "head": branch, "base": base, "body": body, "draft": req.Draft,
	})
	apiBase := p.APIBase
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/repos/%s/%s/pulls", apiBase, owner, repo), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("Content-Type", "application/json")
	client := p.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("provenance: opening the PR: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The branch IS pushed and the body IS ready — hand back the manual
		// gesture instead of failing the whole act.
		out.CompareURL = fmt.Sprintf("https://github.com/%s/%s/compare/%s...%s?expand=1", owner, repo, base, branch)
		return out, fmt.Errorf("provenance: GitHub refused the PR (%d): %s", resp.StatusCode, truncate(string(raw)))
	}
	var created struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(raw, &created); err == nil && created.HTMLURL != "" {
		out.Opened = true
		out.URL = created.HTMLURL
	}
	return out, nil
}
