# Spec: orun-workflows-v3 (the engine comes home)

**v1 made a workflow runnable. v2 made workflows how the platform talks. v3
removes the second binary.** `orun-workflows` (WF0–WF7, v2.32.0) introduced the
third execution vocabulary — `workflow:` — and `orun-workflows-v2` (WX0–WX7)
completed it: a real wire contract with two signatures on it, scoped
connections, declared outputs, a plan-pinned engine, portable workflows, and
resume. That architecture works today; `orun workflow run` executes end to end
through torkflow's `backend` mode.

v3 is not a correction of that work. It is a consequence of a **product
decision**: torkflow is being discontinued as a standalone project. Once the
engine is no longer a separately-maintained, separately-released product, the
process boundary between orun and it stops being an interface and becomes
overhead — and every mechanism built to make that boundary safe becomes
mechanism with nothing left to protect.

v3 vendors the engine into orun, makes the actions orun actually needs
**built-in**, and retires the boundary and everything that guarded it.

## The honest trade

> **This discards working code that shipped recently.** `contract/v1`, the
> `backend` mode, declared outputs and resume all landed in the last cycle
> (torkflow #8/#9/#10, orun #544 and siblings). v3 deletes most of that surface.
> That cost is real and is not being minimised here. It is accepted because the
> code exists to serve a boundary the org has decided not to keep, and carrying
> a wire contract between two halves of one product is a permanent tax paid to
> preserve an option nobody intends to exercise.

What is **not** discarded: the workflow file format (`torkflow/v1`) is unchanged,
so every existing workflow file and its digest stay valid. The declared-outputs
data-flow model and resume semantics are kept — they move in-process, they do
not disappear. v1's defining law is untouched: **only names are intent; values
are execution.**

## What v3 deletes

| Deleted | Why it existed |
|---|---|
| `contract/v1` request/response schemas + fixtures | To version a wire between two binaries |
| torkflow `backend` mode | The counterparty on the far side of that wire |
| Credential marshalling across the boundary | Connections had to be serialised to reach another process |
| Engine digest pinning, OCI engine resolution (v2 §6) | "Which engine ran this" was ambient because the engine was foreign |
| `ORUN_TORKFLOW_ENGINE` | Pointing at a binary orun now contains |
| `actionStore` path discovery | Loose executables resolved from the filesystem at run time |

The engine's digest becomes the orun binary's own digest. There is nothing left
to pin because there is nothing left to point at.

## What v3 adds

- **`core.exec`** — run an argv (no shell), with cwd, an env allowlist, a
  timeout, and captured stdout/stderr/exit. The single missing capability that
  today prevents a workflow from driving `orun`, `git`, `gh` or `pnpm`. This is
  what makes workflows useful for the golden-path flows they were built for.
- **`http.request` as a built-in**, ported from its provider binary. Pure Go;
  there was never a reason for it to be a subprocess.
- **A single distributable.** No action store, no loose binaries, no path
  resolution — the operational story orun already has everywhere else.

## Status

Proposed. Supersedes `orun-workflows-v2` §6 (engine as plan content) and flips
its explicit non-goal *"in-process engine import"*. That non-goal was correct
while torkflow was a live standalone product; v3 exists because that premise
changed. The reversal is recorded in WA0 rather than made silently — the v1/v2
`backend`-mode drift happened precisely because code and spec diverged without a
decision record.

## Read order

1. `design.md` — the architecture: dispatch, the built-in action set, what the
   credential and validation story becomes without a process boundary.
2. `implementation-plan.md` — WA0–WA6, in dependency order.

## Out-of-band references

- `specs/orun-workflows/` — v1, the plan-step and hook surfaces.
- `specs/orun-workflows-v2/` — v2, the contract this epic retires. Its §7
  (workflows travelling in OCI Stacks) is **kept** and is orthogonal to where
  the engine lives.
- torkflow `internal/engine`, `internal/core`, `internal/expression`,
  `internal/executor` — the ~1,250 non-test LOC that move.
