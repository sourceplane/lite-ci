package cliauth

// Rename-stable GitHub repo identity for workspace links. The OIDC exchange
// (orun-cloud identity-worker) resolves a CI workflow's link by the NUMERIC
// repository id from the OIDC token claims — a link created without it passes
// every user-level check (`orun cloud check`) while CI OIDC 404s. So `orun
// cloud link` resolves the identity best-effort at link time and sends it
// with the create; when it cannot (no credential for a private repo), the
// caller warns that CI OIDC needs a console link.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// GitHubRepoIdentity is the rename-stable provider identity of a repo.
type GitHubRepoIdentity struct {
	Provider   string // "github"
	RepoID     string // numeric repository id, decimal string
	OwnerID    string // numeric owner id, decimal string
	OwnerLogin string
}

var githubRemoteRe = regexp.MustCompile(`github\.com[:/]+([^/]+)/([^/]+?)(?:\.git)?$`)

// githubTokenFromEnv resolves a GitHub credential without prompting: the
// conventional env vars first, then a best-effort `gh auth token` (the gh CLI
// keychain) — absent both, empty (public repos still resolve anonymously).
func githubTokenFromEnv() string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	if gh, err := exec.LookPath("gh"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, gh, "auth", "token").Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}

// ResolveGitHubRepoIdentity resolves the numeric repo/owner identity for a
// github.com remote. Nil (with a reason error) when the remote is not
// github.com or the API cannot serve it with the ambient credential.
func ResolveGitHubRepoIdentity(ctx context.Context, remoteURL string) (*GitHubRepoIdentity, error) {
	m := githubRemoteRe.FindStringSubmatch(strings.TrimSpace(remoteURL))
	if m == nil {
		return nil, fmt.Errorf("remote %q is not a github.com repository", remoteURL)
	}
	owner, name := m[1], m[2]

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/"+owner+"/"+name, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := githubTokenFromEnv(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned %d for %s/%s (a private repo needs GITHUB_TOKEN/GH_TOKEN or an authenticated gh)", resp.StatusCode, owner, name)
	}
	var body struct {
		ID    int64 `json:"id"`
		Owner struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
		} `json:"owner"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.ID == 0 {
		return nil, fmt.Errorf("github api returned no repository id")
	}
	return &GitHubRepoIdentity{
		Provider:   "github",
		RepoID:     fmt.Sprintf("%d", body.ID),
		OwnerID:    fmt.Sprintf("%d", body.Owner.ID),
		OwnerLogin: body.Owner.Login,
	}, nil
}
