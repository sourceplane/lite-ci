# Implementation Plan — orun-workflows-v3 (one language, one binary)

Milestone IDs WA0–WA7 (rev 2 renumbers rev 1's WA0–WA6). Each is one
reviewable outcome, ordered so no milestone leaves the tree without a working
`orun workflow run`. The spine: **record the decision → build the language →
build the engine → build the verbs → build the wait → delete the boundary →
open the store → prove it and archive torkflow.**

---

## WA0 — the decision record

**Scope.**
- Amend `orun-workflows-v2`: §6 (engine as plan content) superseded; the
  *"in-process engine import"* non-goal reversed, with the reason (torkflow
  discontinued as a product) recorded, not implied. Confirm §7 survives.
- Record the rev 2 format decision: `torkflow/v1` is convert-supported, never
  authored again; the near-zero installed base is the justification and is
  named as such.
- State the accepted cost: `contract/v1`, `backend` mode, and the boundary
  half of outputs/resume shipped last cycle and are being retired.

**Done when.** v1 → v2 → v3 reads as one coherent story; no document claims an
architecture the code does not have.
**Human help.** Product sign-off that torkflow is discontinued — the epic's
foundation.

---

## WA1 — the language: parse, validate, digest, convert

**Scope.**
- `kind: Workflow` (`orun.dev/v1`) model per design §2: typed `inputs:`
  (reusing `scaffold.InputSpec`), `connections:`, `outputs:`, steps with
  `needs`/`if`/one-verb/`with`/`poll`/`retry`/`outputs`.
- CEL compilation (`cel-go`) for every expression position; `{{ }}`
  interpolation lowered to CEL.
- **`orun workflow validate` becomes real** — this closes a trap hit during
  this epic's own research (a workflow naming a nonexistent action validated
  cleanly): DAG well-formedness (missing/cyclic `needs`, exactly one verb per
  step), every CEL expression compiles, every `steps.X.outputs.Y` /
  `inputs.N` / `connections.N` reference resolves to a declared name, every
  `action:` resolves in the registry, `with:` validates against the action's
  input schema.
- `orun workflow digest` over the canonical new form. `torkflow/v1` input is
  rejected with an error naming design §12 (no converter — WA0 decision).
- CLI symmetry with `orun new`: `orun workflow run --set k=v --values f.yaml`
  feeding `inputs:`.

**Done when.** The design §2 example validates; a file with an unknown action,
an uncompilable expression, or an undeclared reference fails validation with a
line-anchored error; a `torkflow/v1` file is rejected with the §12 error.
**Human help.** None.

---

## WA2 — the engine: in-process DAG execution with resume

**Scope.**
- Scheduler honouring `needs` (AND-join), `if:` skips, per-step
  retry/backoff, `continueOnError`, `maxParallelSteps`. Implementation free to
  diverge from torkflow's (design §8) — **torkflow's scheduler, race and
  resume tests are ported first and must pass**.
- File-backed run state under `.orun/wfruns/<execId>/`; resume re-executes
  only non-succeeded steps (WX6 semantics). Declared outputs evaluated at
  completion; only declared names are sealed.
- `orun workflow run|view` execute in-process. `ORUN_TORKFLOW_ENGINE`, if set,
  still routes outward (deprecated, digest-recorded) — the escape hatch dies
  in WA5, not here.

**Done when.** A multi-step workflow with a mid-run kill resumes re-executing
only what had not succeeded, with no engine binary configured and no
`actionStore` on disk.
**Human help.** None.

---

## WA3 — the verbs: `run:`, built-ins on one dispatch path

**Scope.**
- `run:` per design §6 — argv-only, `cwd` defaulting to the run dir, env
  allowlist, mandatory bounded timeout with kill-on-expiry, `exitCode`/
  `stdout`/`stderr` as expression-visible results.
- Built-ins `http.request` (ported in-process with `http.bearer`), `script`
  (goja, lazy-loaded), `sleep` — all schema-validated and connection-scoped on
  the **single** dispatch path (design §4), closing the built-in-branch bypass
  before it can become the only path.
- Connections resolve exclusively through orun secrets (`secret://`,
  mapped-only, in-memory, redacted). No file-based secret source exists.
- Nested `workflow:` verb with compile-time cycle detection; child digest
  folded into the parent's pin.

**Done when.** A step runs `git`/`gh`/`orun` and a downstream step consumes
its typed outputs; a shell metacharacter in an argv element is provably
literal; an unmapped connection provably never reaches an action.
**Human help.** Confirmation nothing internal consumes `ai.*` (they are not
ported).

---

## WA4 — `poll:` — the wait primitive

**Scope.** Design §7: re-execute the step's verb on `interval` until `until`
is true, `timeout` expires, or a terminal error; every attempt a recorded run
fact; resume re-enters an in-flight poll. Interacts with `retry` (retry wraps
individual attempts; poll wraps the loop).

**Done when.** "Open PR → poll checks until concluded → merge if green" runs
as three steps with no sleep loop in user code, and survives a mid-poll kill +
resume.
**Human help.** None.

---

## WA5 — delete the boundary

**Scope.** Unchanged from rev 1, plus the file-secrets kill:
- Delete `internal/workflowbackend` wire plumbing, `contract/v1` schemas and
  fixtures, `EngineBackendArgs`.
- Delete engine digest pinning / OCI engine resolution
  (`execution.workflowEngine`, `PlanWorkflowEngine`,
  `orun workflow engine-digest`); the intent field is an error immediately
  (design §12 — no compatibility window).
- Delete the `exec.Command` provider protocol, `actionStore` discovery, and
  `ORUN_TORKFLOW_ENGINE`.
- Remove any code path that can read a `connections.yaml`/`secrets.yaml`.

**Done when.** `grep -r "workflowbackend\|actionStore\|TORKFLOW_ENGINE\|secrets.yaml"`
over `internal/ cmd/` returns only the migration warning, with the full
workflow suite green.
**Human help.** None.

---

## WA6 — the store as a registry: OCI action packages

**Scope.** Design §5. Explicitly **deferrable** — nothing earlier depends on
it; ship when the first real third-party action appears, but the design lands
now so WA1's registry namespace does not need re-cutting.
- Package manifest (actions, input schemas, connection types, capability
  grants, payload type), OCI push/pull through `internal/composition`
  fetch+lock, content-addressed cache.
- WASM payload execution via `wazero`, host functions granted per manifest;
  `native: true` process-payload fallback, refusable by policy.
- Package digest materialized into `plan.json`/`provenance.lock` beside the
  workflow digest.

**Done when.** A `slack.post` action from a registry runs sandboxed with only
its granted capabilities, its digest pinned in the plan, with zero loose
executables on disk.
**Human help.** Registry namespace decision (`ghcr.io/sourceplane/orun-actions/*`).

---

## WA7 — prove it, then archive torkflow

**Scope.**
- **The acceptance test is the flow this epic was requested for**: one
  `orun workflow run baseline-flow.yaml` that scaffolds a baseline per phase
  (the split blueprints), opens a PR per phase, `poll:`s CI to green, merges,
  and ends with a verified working repo — no engine binary, no action store,
  no undeclared process.
- Consumer audit of torkflow (GHCR package, `provider.yaml`, kiox pins,
  `ORUN_TORKFLOW_ENGINE` references anywhere); freeze and archive the repo
  with a final release note pointing at orun, only once clear.

**Done when.** The baseline flow passes end-to-end twice — once fresh, once
resumed from an induced mid-flow failure — and torkflow is archived.
**Human help.** Product sign-off on archiving; GitHub permission to archive.

---

## Cross-cutting (every milestone)

- **No milestone leaves `orun workflow run` broken.**
- Semantics are sacred, internals are not: ported torkflow tests are the
  contract of record for scheduler behaviour.
- Every new dependency (`cel-go`, `wazero`; `goja` becomes lazy) is justified
  in the PR that introduces it, with binary-size delta measured.
- The law is not renegotiated: **only names are intent; values are execution.**
