package flow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// fakeGitHub serves the two API shapes FetchRemote uses: raw contents and
// commit resolution.
func fakeGitHub(t *testing.T, flowBody string, wantAuth string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantAuth != "" && r.Header.Get("Authorization") != "Bearer "+wantAuth {
			http.Error(w, "bad credentials", http.StatusUnauthorized)
			return
		}
		switch {
		case strings.Contains(r.URL.Path, "/contents/"):
			if r.URL.Query().Get("ref") == "missing" {
				http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
				return
			}
			w.Write([]byte(flowBody))
		case strings.Contains(r.URL.Path, "/commits/"):
			_ = json.NewEncoder(w).Encode(map[string]string{"sha": "abc123def4567890abc123def4567890abc123de"})
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
}

const remoteFlowBody = `apiVersion: orun.dev/v1
kind: Workflow
metadata:
  name: remote-test
steps:
  - name: hello
    run: ["/bin/sh", "-c", "echo hi"]
`

func TestFetchRemote_GitHubRefWithAuth(t *testing.T) {
	srv := fakeGitHub(t, remoteFlowBody, "tok123")
	defer srv.Close()
	t.Setenv("ORUN_GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "tok123")

	path, meta, cleanup, err := FetchRemote(context.Background(), "github:acme/base@v1.2.3//flows/phases/02-foundation/workflow.yaml")
	if err != nil {
		t.Fatalf("FetchRemote: %v", err)
	}
	defer cleanup()
	if meta.Repo != "acme/base" || meta.Ref != "v1.2.3" {
		t.Errorf("meta = %+v", meta)
	}
	if len(meta.SHA) != 40 {
		t.Errorf("sha not resolved: %q", meta.SHA)
	}
	data, _ := os.ReadFile(path)
	if string(data) != remoteFlowBody {
		t.Errorf("body mismatch: %q", data)
	}
	if wf, err := Parse(data); err != nil || wf.Metadata.Name != "remote-test" {
		t.Errorf("fetched flow does not parse: %v", err)
	}
}

func TestFetchRemote_MissingRefFailsLoudly(t *testing.T) {
	srv := fakeGitHub(t, remoteFlowBody, "")
	defer srv.Close()
	t.Setenv("ORUN_GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	_, _, _, err := FetchRemote(context.Background(), "github:acme/base@missing//flow.yaml")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("error should hint at GITHUB_TOKEN for unauthenticated fetches: %v", err)
	}
}

func TestFetchRemote_InvalidRefShapes(t *testing.T) {
	for _, ref := range []string{"github:acme//flow.yaml", "github:acme/base@v1", "github:onlyrepo//x.yaml"} {
		if _, _, _, err := FetchRemote(context.Background(), ref); err == nil {
			t.Errorf("expected error for %q", ref)
		}
	}
}

func TestRun_ExportsSourceMetaToSteps(t *testing.T) {
	wf := &Workflow{
		Metadata: Metadata{Name: "src-meta"},
		Steps: []Step{
			{Name: "env", Run: []string{"/bin/sh", "-c", "echo repo=$ORUN_FLOW_SOURCE_REPO sha=$ORUN_FLOW_SOURCE_SHA"}},
		},
	}
	res, err := Run(context.Background(), wf, RunOptions{
		Dir:    t.TempDir(),
		Source: &SourceMeta{Repo: "acme/base", Ref: "v1", SHA: "deadbeef"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	st := res.Steps["env"]
	if st == nil || !strings.Contains(st.Stdout, "repo=acme/base") || !strings.Contains(st.Stdout, "sha=deadbeef") {
		t.Fatalf("source meta not in step env: %+v", st)
	}
}
