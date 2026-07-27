# Implementation Plan — orun-workflows-v3 (the engine comes home)

Milestone IDs WA0–WA6. Each is one reviewable outcome, independently valuable,
ordered so every step builds on the previous. The sequence is deliberate:
**record the decision, vendor, add the capability, then remove the boundary** —
removing the wire before the in-process path is proven would leave no working
execution route.

---

## WA0 — the decision record

**Why first.** `orun-workflows-v2` lists *"in-process engine import"* as an
explicit non-goal and prescribes the opposite architecture in its §6. Vendoring
the engine while that text stands would leave the spec actively describing an
architecture the code no longer has — which is exactly how the v1 `backend`-mode
drift happened (the spec claimed a counterparty that did not exist for a full
cycle).

**Scope.**
- Amend `specs/orun-workflows-v2/README.md` + `design.md`: mark §6 (engine as
  plan content) **superseded by v3**, and annotate the non-goal with the reason
  it was reversed — torkflow discontinued as a product, not a defect in v2.
- State the accepted cost in writing: `contract/v1`, `backend` mode, and the
  boundary half of outputs/resume are being retired within a cycle of shipping.
- Confirm v2 §7 (portability) survives.

**Done when.** Reading v2 and v3 in sequence tells one coherent story, and no
document claims an architecture the code does not have.

**Human help.** Product sign-off that torkflow is discontinued — the entire epic
rests on it.

---

## WA1 — vendor the engine, in-process, behind the existing surface

**Why now.** Everything downstream needs a working in-process engine.

**Scope.**
- Vendor into `orun/internal/workflow/`: `engine` (DAG scheduler, run state),
  `expression` (goja), the workflow file model, and the built-in handlers.
  ~1,250 non-test LOC. Add `goja` + `gojsonschema` to `go.mod`.
- Port torkflow's engine and scheduler tests with it. The scheduler had a latent
  data race fixed during WX1 — the race tests come across too.
- `orun workflow run|view` execute in-process. `ORUN_TORKFLOW_ENGINE`, when set,
  still routes to the external engine and records the digest it ran (deprecated
  escape hatch, per design §8).
- The file format is untouched: existing workflows and digests stay valid.

**Done when.** `orun workflow run` executes a workflow with **no engine
configured and no `actionStore` on disk**, using only built-in actions. The
digest of an existing workflow file is unchanged from the pre-vendor value.

**Human help.** None.

---

## WA2 — one dispatch path: schema validation + connections for built-ins

**Why now.** Today the built-in branch bypasses input-schema validation and
credential resolution; both live only on the provider branch
(`scheduler.go:208`). Once every action is built-in, that bypass becomes the
only path — a silent loss of two guarantees. This must land **before** the
provider branch is deleted, not after.

**Scope.**
- Every built-in declares an `inputSchema`; the scheduler validates before
  invoking, uniformly.
- Connections resolve for built-ins exactly as for providers. The v2
  `connections:` grant — compile-checked, mapped-only injection — applies
  unchanged.
- Tests: an unmapped connection is not injected; a schema-violating input fails
  the step before the handler runs.

**Done when.** A built-in action and a provider action are indistinguishable
from the scheduler's contract perspective — same validation, same credential
scoping.

**Human help.** None.

---

## WA3 — `core.exec`

**Why now.** The capability the epic exists for; needs WA2's validation and
credential path to be honest about its inputs.

**Scope.**
- Implement per design §6: argv-only (never a shell), explicit `cwd` defaulting
  to the run dir, env **allowlist** rather than inheritance, mandatory timeout
  with a bounded default and kill-on-expiry, outputs `exitCode`/`stdout`/
  `stderr` through the declared-outputs model, non-zero exit fails the step
  unless an error handler is declared.
- Tests: timeout kills the process; env not in the allowlist is absent; a
  non-zero exit fails; stdout/stderr reach declared outputs; argv is never
  shell-interpreted (a step whose argument contains `;` or `$(…)` passes it
  through as a literal argument).

**Done when.** A workflow step runs `orun`, `git` and `gh` and downstream steps
consume its typed outputs.

**Human help.** None.

---

## WA4 — `http.request` in-process; drop `ai.*` and `demo.*`

**Why now.** The last reason to keep `actionStore` alive.

**Scope.**
- Port `http.request` / `http.request.auth` from the provider binary to
  in-process Go, preserving action names, input schema and the `http.bearer`
  connection type. Existing workflows using them keep working unchanged.
- Drop `ai.*` and `demo.echo` (design §4). Document the rationale and the
  migration answer for `ai.*`: use orun's agent surface.

**Done when.** No shipped workflow path references an action outside the
built-in set.

**Human help.** Confirmation that no internal workflow depends on `ai.*`.

---

## WA5 — delete the boundary

**Why now.** Only safe once WA1–WA4 have made the in-process path complete.

**Scope.**
- Delete `internal/workflowbackend` wire plumbing: `contract/v1` schemas and
  fixtures, request/response marshalling, `EngineBackendArgs`.
- Delete engine digest pinning and OCI engine resolution (v2 §6):
  `execution.workflowEngine` in intent, `PlanWorkflowEngine` in the plan model,
  and `orun workflow engine-digest`. The engine's digest is now the orun
  binary's.
- Delete the `exec.Command` provider protocol and `actionStore` path discovery.
- Retire `ORUN_TORKFLOW_ENGINE` (deprecated in WA1, removed here).
- Migration note for anyone with `execution.workflowEngine` in an intent: it
  becomes an ignored no-op with a warning for one minor version, then an error.

**Done when.** `grep -r workflowbackend\|actionStore\|TORKFLOW_ENGINE` over
`internal/` and `cmd/` returns nothing outside the migration warning, and
`orun workflow run` still passes its full test suite.

**Human help.** None.

---

## WA6 — discontinue torkflow, and prove the thing it was for

**Why now.** Discontinuation is a distribution decision with possible external
consumers; it is gated, not automatic.

**Scope.**
- Audit torkflow's consumers: the GHCR package, `provider.yaml`, any kiox
  provider pin, any repo referencing `ORUN_TORKFLOW_ENGINE`. Freeze the repo
  (archive, final release note pointing at orun) only once that is clear.
- **End-to-end proof** — the flow this epic was requested for: a workflow whose
  nodes are the per-phase blueprint runs, each followed by a PR-open and a
  verify step, driving a baseline instantiation from scaffold to verified repo.
  This is the acceptance test for the whole epic, not a demo.

**Done when.** A single `orun workflow run` creates and verifies a baseline
repo end to end, with no engine binary, no action store, and no external
process other than the tools the workflow explicitly invokes.

**Human help.** Product sign-off on archiving torkflow; GitHub permissions to
archive the repo.

---

## Cross-cutting (every milestone)

- **The format never changes.** Any milestone that would alter `torkflow/v1`
  file semantics is out of scope — existing digests must stay valid throughout.
- **No milestone leaves the tree without a working `orun workflow run`.**
- Tests move with the code they cover; vendored packages arrive with their
  existing suites, not re-written ones.
- v1's law is not renegotiated: **only names are intent; values are execution.**
