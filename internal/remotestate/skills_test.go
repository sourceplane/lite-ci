package remotestate_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/remotestate"
)

// The skills endpoints, pinned to their routes: the org-scoped list and the
// {name}[@rev] read (the @rev pin travels as one path segment).

// recordedCall is the last request a recording server saw.
type recordedCall struct {
	method string
	path   string
	query  string
	body   map[string]interface{}
}

// recordingServer records every request and answers with the given payload
// wrapped in the platform success envelope. It moved here at the work-plane
// teardown (WT2) from the work client's test file, which went with the
// client.
func recordingServer(t *testing.T, payload interface{}) (*httptest.Server, *recordedCall) {
	t.Helper()
	rec := &recordedCall{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.query = r.URL.RawQuery
		rec.body = nil
		if r.Body != nil {
			var m map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&m); err == nil {
				rec.body = m
			}
		}
		writeJSON(w, 200, data(payload))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func scopedTestClient(srv *httptest.Server) *remotestate.Client {
	return remotestate.NewClientWithScope(srv.URL, "test",
		remotestate.NewStaticTokenSource("tok"), remotestate.Scope{OrgID: "acme"})
}

func TestSkillsRoutes(t *testing.T) {
	srv, rec := recordingServer(t, map[string]interface{}{
		"name": "milestone-loop", "rev": "sha256:aa", "source": "org",
		"frontmatter": map[string]interface{}{"title": "The milestone loop"},
		"body":        "# The milestone loop\n",
		"skills": []interface{}{map[string]interface{}{
			"name": "milestone-loop", "rev": "sha256:aa", "source": "default", "frontmatter": map[string]interface{}{},
		}},
	})
	c := scopedTestClient(srv)

	list, err := c.ListSkills(context.Background(), "acme")
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if rec.method != "GET" || rec.path != "/v1/organizations/acme/skills" {
		t.Errorf("list route = %s %s", rec.method, rec.path)
	}
	if len(list.Skills) != 1 || list.Skills[0].Name != "milestone-loop" {
		t.Errorf("list = %+v", list)
	}

	view, err := c.GetSkill(context.Background(), "acme", "milestone-loop", "")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}
	if rec.path != "/v1/organizations/acme/skills/milestone-loop" {
		t.Errorf("get route = %s", rec.path)
	}
	if view.Body == "" || view.Rev != "sha256:aa" {
		t.Errorf("view = %+v", view)
	}

	if _, err := c.GetSkill(context.Background(), "acme", "milestone-loop", "sha256:"+strings.Repeat("b", 64)); err != nil {
		t.Fatalf("GetSkill pinned: %v", err)
	}
	if want := "/v1/organizations/acme/skills/milestone-loop@sha256%3A" + strings.Repeat("b", 64); rec.path != "/v1/organizations/acme/skills/milestone-loop@sha256:"+strings.Repeat("b", 64) {
		t.Errorf("pinned route = %s (want decoded form of %s)", rec.path, want)
	}
}
