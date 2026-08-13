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

// The orun-initiatives-v2 (IS4) client legs: every new method is pinned to
// its route, verb, and body shape — the wire is the contract
// (specs/epics/orun-initiatives-v2/api-and-mcp.md), and a drifted path is a
// silent 404 in production.

type recordedCall struct {
	method string
	path   string
	query  string
	body   map[string]interface{}
}

// workServer records every request and answers with the given payload
// wrapped in the platform success envelope.
func workServer(t *testing.T, payload interface{}) (*httptest.Server, *recordedCall) {
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

func workTestClient(srv *httptest.Server) *remotestate.Client {
	return remotestate.NewClientWithScope(srv.URL, "test",
		remotestate.NewStaticTokenSource("tok"), remotestate.Scope{OrgID: "acme"})
}

func TestIS4ResolverAndContextRoutes(t *testing.T) {
	srv, rec := workServer(t, map[string]interface{}{
		"kind": "task", "key": "ORN-14", "canonicalKey": "ORN-T14", "title": "t", "movedFrom": "ORN-14",
	})
	c := workTestClient(srv)

	item, err := c.GetWorkItem(context.Background(), "ORN-14")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if rec.method != "GET" || rec.path != "/v1/organizations/acme/work/items/ORN-14" {
		t.Errorf("resolver route = %s %s", rec.method, rec.path)
	}
	if item.CanonicalKey != "ORN-T14" || item.MovedFrom != "ORN-14" {
		t.Errorf("resolve = %+v", item)
	}

	if _, err := c.GetWorkContext(context.Background(), "PAY-E2#M1", remotestate.WorkContextOptions{Depth: 3, PerLevel: 10, Activity: 5}); err != nil {
		t.Fatalf("GetWorkContext: %v", err)
	}
	// r.URL.Path arrives decoded: the client escaped the # as %23 (an
	// unescaped # parses as a fragment and the request would silently aim
	// at the epic), and the server decodes it back to the milestone ref.
	if rec.path != "/v1/organizations/acme/work/items/PAY-E2#M1/context" {
		t.Errorf("context route = %s (the milestone # must survive the wire)", rec.path)
	}
	for _, want := range []string{"depth=3", "perLevel=10", "activity=5"} {
		if !strings.Contains(rec.query, want) {
			t.Errorf("context query %q lacks %s", rec.query, want)
		}
	}
}

func TestIS4StateRoutesAndTokenDefaulting(t *testing.T) {
	srv, rec := workServer(t, map[string]interface{}{"key": "pay", "seq": 7, "status": "active"})
	c := workTestClient(srv)

	if _, err := c.SetInitiativeStatus(context.Background(), "pay", remotestate.SetInitiativeStatusRequest{To: "active"}); err != nil {
		t.Fatalf("SetInitiativeStatus: %v", err)
	}
	if rec.method != "POST" || rec.path != "/v1/organizations/acme/work/initiatives/pay/status" {
		t.Errorf("status route = %s %s", rec.method, rec.path)
	}
	// IS-L: the client defaults the idempotency token on — the transport
	// retry is replay-safe by construction.
	tok, _ := rec.body["clientToken"].(string)
	if !strings.HasPrefix(tok, "ct_") || len(tok) != 3+32 {
		t.Errorf("clientToken not defaulted: %q", tok)
	}

	// A caller-supplied token passes through verbatim.
	if _, err := c.SetInitiativeStatus(context.Background(), "pay", remotestate.SetInitiativeStatusRequest{To: "paused", ClientToken: "ct_mine"}); err != nil {
		t.Fatalf("SetInitiativeStatus: %v", err)
	}
	if tok, _ := rec.body["clientToken"].(string); tok != "ct_mine" {
		t.Errorf("caller token rewritten: %q", tok)
	}

	if _, err := c.PostInitiativeUpdate(context.Background(), "pay", remotestate.PostInitiativeUpdateRequest{Health: "at_risk", Body: "b"}); err == nil {
		if rec.path != "/v1/organizations/acme/work/initiatives/pay/updates" || rec.body["health"] != "at_risk" {
			t.Errorf("updates route = %s body %v", rec.path, rec.body)
		}
		if tok, _ := rec.body["clientToken"].(string); !strings.HasPrefix(tok, "ct_") {
			t.Errorf("update token not defaulted: %v", rec.body)
		}
	}

	if _, err := c.ListInitiativeUpdates(context.Background(), "pay"); err == nil {
		if rec.method != "GET" || rec.path != "/v1/organizations/acme/work/initiatives/pay/updates" {
			t.Errorf("updates read route = %s %s", rec.method, rec.path)
		}
	}

	if _, err := c.SetInitiativeArchived(context.Background(), "pay", true); err == nil {
		if rec.path != "/v1/organizations/acme/work/initiatives/pay/archive" {
			t.Errorf("archive route = %s", rec.path)
		}
	}
	if _, err := c.SetInitiativeArchived(context.Background(), "pay", false); err == nil {
		if rec.path != "/v1/organizations/acme/work/initiatives/pay/unarchive" {
			t.Errorf("unarchive route = %s", rec.path)
		}
	}
}

func TestIS4VoiceAndAssignRoutes(t *testing.T) {
	srv, rec := workServer(t, map[string]interface{}{"key": "ORN-1", "seq": 9})
	c := workTestClient(srv)

	if _, err := c.AssertTaskDone(context.Background(), "ORN-1", "docs shipped", ""); err != nil {
		t.Fatalf("AssertTaskDone: %v", err)
	}
	if rec.path != "/v1/organizations/acme/work/tasks/ORN-1/done" || rec.body["note"] != "docs shipped" {
		t.Errorf("done route = %s body %v", rec.path, rec.body)
	}
	if tok, _ := rec.body["clientToken"].(string); !strings.HasPrefix(tok, "ct_") {
		t.Errorf("done token not defaulted: %v", rec.body)
	}

	if _, err := c.PostTaskNote(context.Background(), "ORN-1", remotestate.PostTaskNoteRequest{Text: "tests green", Ref: "abc"}); err != nil {
		t.Fatalf("PostTaskNote: %v", err)
	}
	if rec.path != "/v1/organizations/acme/work/tasks/ORN-1/note" || rec.body["text"] != "tests green" || rec.body["ref"] != "abc" {
		t.Errorf("note route = %s body %v", rec.path, rec.body)
	}

	if _, err := c.AssignWorkItem(context.Background(), "pay", remotestate.AssignWorkItemRequest{Subject: "usr_7", Override: "gate note"}); err != nil {
		t.Fatalf("AssignWorkItem: %v", err)
	}
	if rec.path != "/v1/organizations/acme/work/items/pay/assign" || rec.body["subject"] != "usr_7" || rec.body["override"] != "gate note" {
		t.Errorf("assign route = %s body %v", rec.path, rec.body)
	}
}

func TestIS4NowRoute(t *testing.T) {
	srv, rec := workServer(t, map[string]interface{}{"rows": []interface{}{}})
	c := workTestClient(srv)
	if _, err := c.GetWorkNow(context.Background(), remotestate.WorkNowOptions{Initiative: "pay", Seat: "sp_1", Limit: 5}); err != nil {
		t.Fatalf("GetWorkNow: %v", err)
	}
	if rec.method != "GET" || rec.path != "/v1/organizations/acme/work/now" {
		t.Errorf("now route = %s %s", rec.method, rec.path)
	}
	for _, want := range []string{"initiative=pay", "seat=sp_1", "limit=5"} {
		if !strings.Contains(rec.query, want) {
			t.Errorf("now query %q lacks %s", rec.query, want)
		}
	}
	// IS-Q: a positive After emits the long-poll pair; zero emits neither
	// (seq 0 = no watermark yet, an ordinary immediate read).
	if strings.Contains(rec.query, "after=") {
		t.Errorf("now query %q long-polls without a watermark", rec.query)
	}
	if _, err := c.GetWorkNow(context.Background(), remotestate.WorkNowOptions{After: 41, WaitSeconds: 20}); err != nil {
		t.Fatalf("GetWorkNow long-poll: %v", err)
	}
	for _, want := range []string{"after=41", "waitSeconds=20"} {
		if !strings.Contains(rec.query, want) {
			t.Errorf("now long-poll query %q lacks %s", rec.query, want)
		}
	}
}

// TestIS2bYoursRoute: the addressed queue read — route, paging, and the
// long-poll pair; the watermark and items decode.
func TestIS2bYoursRoute(t *testing.T) {
	srv, rec := workServer(t, map[string]interface{}{
		"items": []interface{}{map[string]interface{}{
			"id": "att:approval_drifted:checkout:usr_dana", "person": "usr_dana",
			"kind": "approval_drifted", "reason": "checkout approval drifted",
			"since": "2026-08-10T00:00:00Z", "source": "work",
			"subject": map[string]interface{}{"key": "checkout", "publicId": "epc_1", "initiative": "pay"},
			"act":     map[string]interface{}{"tool": "epic_approve", "url": "/work/items/checkout"},
		}},
		"seq": 77,
	})
	c := workTestClient(srv)
	queue, err := c.GetWorkYours(context.Background(), remotestate.WorkYoursOptions{Limit: 10, After: 50, WaitSeconds: 25})
	if err != nil {
		t.Fatalf("GetWorkYours: %v", err)
	}
	if rec.method != "GET" || rec.path != "/v1/organizations/acme/work/yours" {
		t.Errorf("yours route = %s %s", rec.method, rec.path)
	}
	for _, want := range []string{"limit=10", "after=50", "waitSeconds=25"} {
		if !strings.Contains(rec.query, want) {
			t.Errorf("yours query %q lacks %s", rec.query, want)
		}
	}
	if queue.Seq != 77 || len(queue.Items) != 1 {
		t.Fatalf("yours = %+v", queue)
	}
	item := queue.Items[0]
	if item.Person != "usr_dana" || item.Act.Tool != "epic_approve" || item.Subject.Initiative != "pay" {
		t.Errorf("attention item = %+v", item)
	}
}

func TestIS4DecisionRoutes(t *testing.T) {
	srv, rec := workServer(t, map[string]interface{}{
		"key": "demo-epic", "seq": 3, "snapshot": "sha256:snap",
		"minted": []interface{}{}, "tasks": []interface{}{},
	})
	c := workTestClient(srv)

	if _, err := c.RequestWorkReview(context.Background(), "designs", "DSG-1", remotestate.WorkReviewRequest{Note: "look"}); err != nil {
		t.Fatalf("RequestWorkReview: %v", err)
	}
	if rec.path != "/v1/organizations/acme/work/designs/DSG-1/review" {
		t.Errorf("review route = %s", rec.path)
	}
	if _, err := c.SubmitWorkVerdict(context.Background(), "", "demo-epic", remotestate.WorkVerdictRequest{Verdict: "approve", Note: "sound"}); err != nil {
		t.Fatalf("SubmitWorkVerdict: %v", err)
	}
	// Empty collection defaults to epics.
	if rec.path != "/v1/organizations/acme/work/epics/demo-epic/verdict" || rec.body["verdict"] != "approve" {
		t.Errorf("verdict route = %s body %v", rec.path, rec.body)
	}
	if _, err := c.RequestWorkReview(context.Background(), "nonsense", "k", remotestate.WorkReviewRequest{}); err == nil {
		t.Error("unknown collection must refuse before the wire")
	}

	out, err := c.ApproveEpic(context.Background(), "demo-epic", remotestate.ApproveEpicRequest{Revision: "sha256:b2d4"})
	if err != nil {
		t.Fatalf("ApproveEpic: %v", err)
	}
	if rec.path != "/v1/organizations/acme/work/epics/demo-epic/approve" || rec.body["revision"] != "sha256:b2d4" {
		t.Errorf("approve route = %s body %v", rec.path, rec.body)
	}
	if out.Snapshot != "sha256:snap" {
		t.Errorf("approve snapshot = %q — the approval IS the dispatch artifact", out.Snapshot)
	}

	if _, err := c.RevokeEpicApproval(context.Background(), "demo-epic", "scope changed"); err == nil {
		if rec.path != "/v1/organizations/acme/work/epics/demo-epic/revoke-approval" || rec.body["note"] != "scope changed" {
			t.Errorf("revoke route = %s body %v", rec.path, rec.body)
		}
	}

	if _, err := c.AdoptDesign(context.Background(), "DSG-1", remotestate.AdoptDesignRequest{TaskPrefix: "PAY"}); err != nil {
		t.Fatalf("AdoptDesign: %v", err)
	}
	if rec.path != "/v1/organizations/acme/work/designs/DSG-1/adopt" || rec.body["taskPrefix"] != "PAY" {
		t.Errorf("adopt route = %s body %v", rec.path, rec.body)
	}

	if _, err := c.SupersedeDesign(context.Background(), "DSG-1", remotestate.SupersedeDesignRequest{By: "DSG-2"}); err != nil {
		t.Fatalf("SupersedeDesign: %v", err)
	}
	if rec.path != "/v1/organizations/acme/work/designs/DSG-1/supersede" || rec.body["by"] != "DSG-2" {
		t.Errorf("supersede route = %s body %v", rec.path, rec.body)
	}
}

// TestReviewCollectionOf pins the key-grammar routing: design keys — typed
// (PFX-D<n>), legacy (DSG-<n>), machine (dsg_…) — ride /designs; slugs and
// every other key ride /epics.
func TestReviewCollectionOf(t *testing.T) {
	cases := map[string]string{
		"dsg_01ABC":  "designs",
		"DSG-3":      "designs",
		"PAY-D12":    "designs",
		"PAY-T14":    "epics",
		"demo-epic":  "epics",
		"PAY-DX":     "epics", // -D must be followed by digits only
		"PAY-D":      "epics", // bare -D suffix is not a design key
		"checkout-D": "epics",
	}
	for key, want := range cases {
		if got := remotestate.ReviewCollectionOf(key); got != want {
			t.Errorf("ReviewCollectionOf(%q) = %s, want %s", key, got, want)
		}
	}
}
