# Spec: orun-initiatives-v2 (initiative-grade state — orun half)

**The plane gains a spine and the agent gains a pen. Initiative status
and health become stored speech acts (the delivery fold stays
unwritable); every noun gets one self-describing key grammar
(`PAY` · `PAY-D1` · `PAY-E2` · `PAY-E2#M1` · `PAY-T14`) over a machine-id
rail; the work MCP grows 21 → 37 under allow/ask/deny trust tiers where
an ask-confirmation is the human signature, every write is
`clientToken`-idempotent, and `sp_` seats can never sign; `work_context` primes any agent from any key (ancestry up, bounded
subtree down, budgets echoed); the **worklog** gives every in-flight
task a live, rate-clamped, fold-inert *now* line (`task_note` /
`work_now`); and `orun pr open` becomes the provenance pen the cloud's
`orun/compliance` check-run verifies.**

> **The authoritative epic lives in orun-cloud**
> (`specs/epics/orun-initiatives-v2/`: README, design, api-and-mcp —
> cluster **IS**, milestones IS0→IS8). This folder is the orun half.
> The wire contract is `specs/epics/orun-initiatives-v2/api-and-mcp.md`;
> field-level truth follows in `packages/contracts/src/work.ts` as
> milestones land.

## Status

| Field | Value |
|-------|-------|
| Status | **In flight — IS6b** (IS4 ✅, IS5 ✅, IS6a ✅ #621; this slice: the provenance pen — `orun pr open\|check\|link`, `pr_open` completing the roster at **37**, `orun githooks install`; IS7 compliance App next) |
| Builds on | `specs/orun-work/` (v2 fold + two logs), `specs/orun-work-v4/` (hierarchy, human-only sealed approval, drift), `specs/orun-initiatives/` (the 21-tool roster, `orun initiatives` group) |
| Coordinates with | orun-cloud `specs/epics/orun-initiatives-v2/` (authoritative: schema, endpoints, compliance App, skills registry, console) |
| Wire | `/v1/organizations/{org}/work/*` — unchanged prefix, additive only (IN-A carried) |
| Milestone prefix | **IS** (this repo's legs land inside IS4, IS5, IS6, IS8) |

## What this repo owns

1. **The wire client grows** (`internal/remotestate/work.go`): the
   universal resolver + context bundle reads (`GetWorkItem`,
   `GetWorkContext`); status/updates/archive
   (`SetInitiativeStatus`, `PostInitiativeUpdate`, `ListInitiativeUpdates`);
   the agent's voice + generalized assign (`AssertTaskDone`,
   `PostTaskNote`, `GetWorkNow`, `AssignWorkItem`); and clients for the
   v4 endpoints that never had them (`RequestWorkReview`,
   `SubmitWorkVerdict`, `ApproveEpic`, `RevokeEpicApproval`,
   `AdoptDesign`, `SupersedeDesign`). Reads retryable, writes not, as
   everywhere; the three hot reads (`yours`/`now`/`activity`) support
   the IS-Q bounded long-poll (`after` + `waitSeconds ≤ 25`), and
   property edits send `ifSeq` by default (409 on a lost race, legible
   retry).
2. **The work MCP grows 21 → 37 under tiers** (`internal/workmcp`):
   `work_context`, `work_now`, `work_yours` (the addressed personal
   queue), `initiative_updates_get` (reads);
   `item_assign` (absorbing `task_assign`, which stays registered),
   `review_request`, `review_verdict`, `task_done`, `task_note`,
   `initiative_update_post`, `initiative_status_set`, `design_adopt`,
   `design_supersede`, `epic_approve`, `epic_revoke_approval`, `pr_open`
   (writes; `task_note` allow-tier for `sp_` seats — narration is
   exactly what autonomous seats owe; every write schema carries
   `clientToken` and the client defaults it on — retries are safe by
   construction). Tier plumbing: allow/ask/deny per agent type; **ask**
   surfaces a harness confirmation and proceeds under the *user
   principal* with `via: mcp/<session>` — the confirmation is the
   signature; for `sp_` seats ask resolves to deny. `human_only`
   refusals pass through verbatim, code included (IN-4 carried).
   Forbidden-fragment sweep updated: `approve`/`adopt` become legitimate
   tool names; `pin` and any rung-write stay forbidden.
3. **CLI verbs** (`cmd/orun/initiatives.go` +new `cmd/orun/pr.go`):
   `start|pause|resume|complete|cancel|reopen`, `update`/`updates`,
   `context`, `assign`, `review request|verdict`, `adopt` (interactive
   confirm = the signature), `task done`, `task note` (the worklog),
   `now` (the live board), `yours` (the addressed queue), list filters;
   `orun pr open|check|link` (incl. `--quick`, minting a `WRK` triage
   task inline), `orun githooks install`, `orun skills list|pull`.
4. **The provenance pen + preflight** (IS6): branch grammar
   (`orun/<task-key>-<slug>`), manifest v1, `Orun-Task:` trailer, and the
   **shared compliance rule engine** — the Go evaluation of the same
   rule spec the cloud App runs, pinned byte-identical by shared
   conformance fixtures (the worklens pattern, applied twice).
5. **Skills materialization** (IS5): `orun agent run` fetches the pinned
   skill revisions and writes them as native skill files beside the MCP
   config it already writes; the session records the revisions for the
   PR manifest.
6. **Agent-type policies** (`agents/*.md` + `internal/agent/mcp.go`):
   the tier matrix for `implementer`/`orchestrator`/`bootstrapper` —
   closing the shipped deny-by-omission gap on the six IN-era tools —
   plus the new **`initiative-owner`** type that runs the full loop.
7. **The oracle grows in lockstep** (`internal/worklens`): the status
   machine, staleness, signals rename, `done_asserted` weakest-voice
   rule, `progress_noted` as a fixture-pinned fold no-op, typed-key
   validation; `hierarchy-conformance.json` stays byte-identical with
   the cloud or the build fails.
8. **Docs**: website pages for every new verb; the MCP page's roster
   table 21 → 34; release notes.

## Invariants this repo enforces (beyond v2/v4/IN's, which all stand)

1. The delivery fold gains exactly one rule (`done_asserted`, weakest
   voice) and one deliberate no-op (`progress_noted` — narration moves
   nothing) and nothing else; `released` stays evidence-only; all three
   facts fixture-pinned.
2. No tool renamed, ever; growth additive; `task_assign` forwards.
3. An `sp_` seat cannot reach a signature by any composition of tools —
   asserted by test against the full 37-tool roster.
4. Every MCP-originated event carries `via`; events without attribution
   fail validation locally before the wire.
5. `orun pr check` and the cloud evaluator return byte-identical
   verdicts on the shared fixtures.
6. `orun spec pull` / `orun epic pull` accept typed keys and slugs alike
   (alias resolution), byte-identical output for byte-identical inputs.
7. CLI rendering obeys the IS-O chip language: every state word prints
   with its source badge (`signed/authored/derived/asserted`) — the
   existing rung-ladder and intent-chip renderers grow the badge, no
   chip prints bare.

## Read order

1. orun-cloud `specs/epics/orun-initiatives-v2/README.md` — decisions
   IS-A…IS-I, invariants, milestones.
2. orun-cloud `specs/epics/orun-initiatives-v2/design.md` — the
   normative model (state machine §1–3, key grammar §4, context read §5,
   trust model §6, provenance §9).
3. orun-cloud `specs/epics/orun-initiatives-v2/api-and-mcp.md` — the
   wire surface this repo implements against.
4. This file — the orun-half ownership and enforcement points.
5. `specs/orun-work/`, `specs/orun-work-v4/`, `specs/orun-initiatives/`
   — the substrate this never breaks.
