package worklens

import (
	"encoding/json"
	"testing"
)

func TestEventValidate(t *testing.T) {
	base := CoordinationEvent{
		Workspace: "ws", Subject: "ORN-1", Kind: EventCommentAdded,
		Actor: Actor{Type: ActorUser, ID: "usr_1"}, At: "2026-07-02T09:00:00Z",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}

	e := base
	e.Actor = Actor{}
	if err := e.Validate(); err == nil {
		t.Error("actor-less event accepted (invariant 3)")
	}

	e = base
	e.Kind = "rung_set" // a task-rung write must never exist (WP-3, narrowed by IS2)
	if err := e.Validate(); err == nil {
		t.Error("unknown event kind accepted (closed vocabulary)")
	}

	e = base
	e.Subject = ""
	if err := e.Validate(); err == nil {
		t.Error("subject-less event accepted")
	}

	// The category "agent lies about lifecycle" is unrepresentable: there is
	// no lifecycle-write kind, and agents may not pin (WP-10).
	pin := base
	pin.Kind = EventPinned
	pin.Actor = Actor{Type: ActorAgent, ID: "sp_1"}
	pin.Payload = json.RawMessage(`{"rung":"done"}`)
	if err := pin.Validate(); err == nil {
		t.Error("agent pin accepted (WP-10)")
	}
	pin.Actor = Actor{Type: ActorUser, ID: "usr_1"}
	if err := pin.Validate(); err != nil {
		t.Errorf("human pin rejected: %v", err)
	}
}

func TestNoLifecycleWriteKindExists(t *testing.T) {
	// WP-3, narrowed by IS2 (orun-initiatives-v2): "no stored status"
	// became "no stored DELIVERY status". status_changed exists since IS2,
	// but it is the INITIATIVE lifecycle stage — a speech act, payload-
	// gated human-only into/out of terminal states (IS-4) — never a task
	// rung. The task-rung write stays unrepresentable.
	for _, k := range EventKinds {
		if k == "lifecycle_changed" || k == "rung_set" || k == "rung_changed" || k == "status_set" || k == "task_status_changed" {
			t.Fatalf("task-rung write kind %q exists — delivery lifecycle is a derived query (WP-3)", k)
		}
	}
}

func TestStatusChangedTerminalMovesAreHumanOnly(t *testing.T) {
	// IS-4: entering or leaving completed/canceled is a signature; the
	// working moves stay open to attributed agents.
	move := func(from, to string, actor Actor) error {
		e := CoordinationEvent{
			Workspace: "ws", Subject: "payments", Kind: EventStatusChanged,
			Actor: actor, At: "2026-08-01T10:00:00Z", Seq: 1,
			Payload: json.RawMessage(`{"from":"` + from + `","to":"` + to + `"}`),
		}
		return e.Validate()
	}
	agent := Actor{Type: ActorAgent, ID: "sp_1"}
	user := Actor{Type: ActorUser, ID: "usr_1"}
	if err := move("planning", "active", agent); err != nil {
		t.Errorf("agent start rejected: %v", err)
	}
	if err := move("active", "paused", agent); err != nil {
		t.Errorf("agent pause rejected: %v", err)
	}
	for _, m := range [][2]string{{"active", "completed"}, {"active", "canceled"}, {"completed", "active"}, {"canceled", "planning"}} {
		if err := move(m[0], m[1], agent); err == nil {
			t.Errorf("agent %s → %s accepted — terminal moves are human-only (IS-4)", m[0], m[1])
		}
		if err := move(m[0], m[1], user); err != nil {
			t.Errorf("human %s → %s rejected: %v", m[0], m[1], err)
		}
	}
}

func TestDoneAssertedNeedsANote(t *testing.T) {
	e := CoordinationEvent{
		Workspace: "ws", Subject: "ORN-T1", Kind: EventDoneAsserted,
		Actor: Actor{Type: ActorAgent, ID: "sp_1"}, At: "2026-08-01T10:00:00Z", Seq: 1,
		Payload: json.RawMessage(`{}`),
	}
	if err := e.Validate(); err == nil {
		t.Error("note-less assertion accepted — an assertion without a reason is a status write (design §7.1)")
	}
	e.Payload = json.RawMessage(`{"note":"published by hand"}`)
	if err := e.Validate(); err != nil {
		t.Errorf("noted assertion rejected: %v", err)
	}
}

func TestV3VocabularyAcceptedAtWriteTime(t *testing.T) {
	// orun-work-v3 PM0: the coordination vocabulary grew to 19; every
	// addition is intent or conversation (V3-1). Write-time acceptance only —
	// the fold reads none of them (the shared conformance fixtures pin the
	// lifecycle logic byte-identically). The observation vocabulary is frozen.
	// (The total count is asserted by the v4 test — TestV4VocabularyAndGuards.)
	if got := len(ObservationKinds); got != 6 {
		t.Fatalf("ObservationKinds = %d, want 6 (frozen in v3 — V3-1)", got)
	}
	for _, k := range []EventKind{
		EventDocEdited, EventReactionAdded, EventReactionRemoved,
		EventLabeled, EventUnlabeled, EventPrioritized,
		EventEstimated, EventCycleSet, EventRelated, EventUnrelated,
	} {
		if !IsEventKind(k) {
			t.Errorf("v3 kind %q not accepted", k)
		}
		e := CoordinationEvent{
			Workspace: "ws", Subject: "OGP-1", Kind: k,
			Actor: Actor{Type: ActorUser, ID: "usr_1"}, At: "2026-07-09T00:00:00Z", Seq: 1,
		}
		if err := e.Validate(); err != nil {
			t.Errorf("v3 kind %q rejected at write time: %v", k, err)
		}
	}
}

func TestObservationValidate(t *testing.T) {
	base := Observation{
		Workspace: "ws", Source: "github-webhook", SourceVersion: 1,
		Kind: ObsPROpened, At: "2026-07-02T09:00:00Z", DedupeKey: "gh:pr:o/r#1:opened",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid observation rejected: %v", err)
	}
	for name, mutate := range map[string]func(*Observation){
		"unknown kind":     func(o *Observation) { o.Kind = "deploy_attempted" },
		"missing source":   func(o *Observation) { o.Source = "" },
		"missing version":  func(o *Observation) { o.SourceVersion = 0 },
		"missing dedupeKey": func(o *Observation) { o.DedupeKey = "" },
	} {
		o := base
		mutate(&o)
		if err := o.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestContractComplete(t *testing.T) {
	if (&Contract{}).Complete() {
		t.Error("empty contract complete")
	}
	var nilC *Contract
	if nilC.Complete() {
		t.Error("nil contract complete")
	}
	full := &Contract{Goal: "g", Affects: []string{"a/b/c"}, DoneWhen: []string{"d"}, Gates: []string{"tests"}}
	if !full.Complete() {
		t.Error("full contract incomplete")
	}
	emptyGates := &Contract{Goal: "g", Affects: []string{"a/b/c"}, DoneWhen: []string{"d"}, GatesDefined: true}
	if !emptyGates.Complete() {
		t.Error("explicit empty gate set should count as declared (P-7)")
	}
	undeclared := &Contract{Goal: "g", Affects: []string{"a/b/c"}, DoneWhen: []string{"d"}}
	if undeclared.Complete() {
		t.Error("undeclared gates should be incomplete")
	}
}

func TestKeysAndSlugs(t *testing.T) {
	if p, s, ok := ParseTaskKey("ORN-142"); !ok || p != "ORN" || s != "142" {
		t.Errorf("ParseTaskKey(ORN-142) = %s %s %v", p, s, ok)
	}
	for _, bad := range []string{"orn-1", "TOOLONGG-1", "ORN-0", "ORN", "A-1"} {
		if _, _, ok := ParseTaskKey(bad); ok {
			t.Errorf("ParseTaskKey(%q) accepted", bad)
		}
	}
	keys := TaskKeysIn("feat/ORN-3-wire lands WP0 with ORN-12 and ORN-3 again")
	if len(keys) != 2 || keys[0] != "ORN-3" || keys[1] != "ORN-12" {
		t.Errorf("TaskKeysIn = %v", keys)
	}
	if !ValidSlug("orun-work") || ValidSlug("Orun Work") {
		t.Error("slug validation wrong")
	}
	if !ValidPrefix("ORN") || ValidPrefix("O") || ValidPrefix("TOOLONGG") {
		t.Error("prefix validation wrong")
	}
}

func TestEnvelopeValidate(t *testing.T) {
	spec := Spec{APIVersion: APIVersion, Kind: KindSpec, Key: "sourceplane/specs/orun-work", Workspace: "ws_1", Title: "t", CreatedBy: Actor{Type: ActorUser, ID: "u"}}
	if err := ValidateSpec(spec); err != nil {
		t.Errorf("valid spec rejected: %v", err)
	}
	task := Task{APIVersion: APIVersion, Kind: KindTask, Key: "ORN-1", Workspace: "ws_1", Title: "t", CreatedBy: Actor{Type: ActorUser, ID: "u"}}
	if err := ValidateTask(task); err != nil {
		t.Errorf("valid task rejected: %v", err)
	}
	task.Key = "not-a-key"
	if err := ValidateTask(task); err == nil {
		t.Error("bad task key accepted")
	}
}
