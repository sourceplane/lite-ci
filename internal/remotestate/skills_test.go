package remotestate_test

import (
	"context"
	"strings"
	"testing"
)

// The IS5 skills endpoints, pinned to their routes: the org-scoped list
// and the {name}[@rev] read (the @rev pin travels as one path segment).

func TestSkillsRoutes(t *testing.T) {
	srv, rec := workServer(t, map[string]interface{}{
		"name": "milestone-loop", "rev": "sha256:aa", "source": "org",
		"frontmatter": map[string]interface{}{"title": "The milestone loop"},
		"body":        "# The milestone loop\n",
		"skills": []interface{}{map[string]interface{}{
			"name": "milestone-loop", "rev": "sha256:aa", "source": "default", "frontmatter": map[string]interface{}{},
		}},
	})
	c := workTestClient(srv)

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
