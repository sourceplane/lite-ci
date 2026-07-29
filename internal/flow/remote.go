package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SourceMeta records where a remotely fetched workflow came from — repo,
// the ref as given, and the resolved commit SHA. It is exported to every
// run: step as ORUN_FLOW_SOURCE_{REPO,REF,SHA} so a flow can self-pin: fetch
// its own baseline at EXACTLY the commit the flow file was fetched from,
// making one remote reference transitively pin flow + scripts + content.
type SourceMeta struct {
	Repo string // owner/name ("" for plain https URLs)
	Ref  string // ref as requested ("" = default branch)
	SHA  string // resolved commit SHA ("" when resolution was unavailable)
	URL  string // https source URL (plain-URL fetches)
}

// IsRemoteRef reports whether the argument to `workflow run` names a remote
// workflow rather than a local file.
func IsRemoteRef(ref string) bool {
	return strings.HasPrefix(ref, "github:") ||
		strings.HasPrefix(ref, "https://") ||
		strings.HasPrefix(ref, "http://")
}

// githubAPIBase is overridable for tests (and GHES) via ORUN_GITHUB_API_URL.
func githubAPIBase() string {
	if v := strings.TrimSpace(os.Getenv("ORUN_GITHUB_API_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.github.com"
}

func githubToken() string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// FetchRemote downloads a remote workflow reference to a temp file and
// returns its local path plus source metadata. Supported schemes:
//
//	github:owner/repo@ref//path/to/workflow.yaml   (ref optional: @ref may be omitted)
//	https://…/workflow.yaml                        (fetched verbatim)
//
// GitHub fetches authenticate with GITHUB_TOKEN/GH_TOKEN when set (required
// for private repos) and also resolve the ref to a commit SHA.
func FetchRemote(ctx context.Context, ref string) (string, *SourceMeta, func(), error) {
	client := &http.Client{Timeout: 60 * time.Second}
	noop := func() {}

	get := func(url string, headers map[string]string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s: HTTP %d: %s", url, resp.StatusCode, firstLine(body))
		}
		return body, nil
	}

	writeTemp := func(data []byte) (string, func(), error) {
		dir, err := os.MkdirTemp("", "orun-remote-flow-")
		if err != nil {
			return "", noop, err
		}
		p := filepath.Join(dir, "workflow.yaml")
		if err := os.WriteFile(p, data, 0o600); err != nil {
			_ = os.RemoveAll(dir)
			return "", noop, err
		}
		return p, func() { _ = os.RemoveAll(dir) }, nil
	}

	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		body, err := get(ref, nil)
		if err != nil {
			return "", nil, noop, fmt.Errorf("fetch workflow: %w", err)
		}
		p, cleanup, err := writeTemp(body)
		if err != nil {
			return "", nil, noop, err
		}
		return p, &SourceMeta{URL: ref}, cleanup, nil
	}

	// github:owner/repo[@ref]//path
	rest := strings.TrimPrefix(ref, "github:")
	repoAndRef, path, ok := strings.Cut(rest, "//")
	if !ok || path == "" {
		return "", nil, noop, fmt.Errorf("invalid github ref %q: expected github:owner/repo[@ref]//path/to/workflow.yaml", ref)
	}
	repo, gitRef, _ := strings.Cut(repoAndRef, "@")
	if parts := strings.Split(repo, "/"); len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", nil, noop, fmt.Errorf("invalid github ref %q: repo must be owner/name", ref)
	}

	headers := map[string]string{"Accept": "application/vnd.github.raw"}
	if tok := githubToken(); tok != "" {
		headers["Authorization"] = "Bearer " + tok
	}
	fileURL := fmt.Sprintf("%s/repos/%s/contents/%s", githubAPIBase(), repo, strings.TrimPrefix(path, "/"))
	if gitRef != "" {
		fileURL += "?ref=" + gitRef
	}
	body, err := get(fileURL, headers)
	if err != nil {
		hint := ""
		if githubToken() == "" {
			hint = " (private repo? set GITHUB_TOKEN)"
		}
		return "", nil, noop, fmt.Errorf("fetch workflow%s: %w", hint, err)
	}

	meta := &SourceMeta{Repo: repo, Ref: gitRef}
	// Resolve the ref to a commit SHA — best-effort: the flow still runs if
	// resolution fails, it just cannot self-pin to an exact commit.
	shaRef := gitRef
	if shaRef == "" {
		shaRef = "HEAD"
	}
	shaHeaders := map[string]string{"Accept": "application/vnd.github+json"}
	if tok := githubToken(); tok != "" {
		shaHeaders["Authorization"] = "Bearer " + tok
	}
	if shaBody, shaErr := get(fmt.Sprintf("%s/repos/%s/commits/%s", githubAPIBase(), repo, shaRef), shaHeaders); shaErr == nil {
		var commit struct {
			SHA string `json:"sha"`
		}
		if json.Unmarshal(shaBody, &commit) == nil {
			meta.SHA = commit.SHA
		}
	}

	p, cleanup, err := writeTemp(body)
	if err != nil {
		return "", nil, noop, err
	}
	return p, meta, cleanup, nil
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
