# Spec: orun-initiatives (the Initiatives surface — orun half)

**Work gets one name and one home: Initiatives. The truth model does not
move — lifecycle stays the v2 derived fold, approval stays v4's human-only
sealed decision — but the surface over it consolidates: four new derived
reads (portfolio, tree, task detail, tagged activity), a work MCP that
grows from 15 to 21 tools with typed `human_only` refusals for the
decisions agents may not make, and a CLI group `orun initiatives` that
replaces `orun work` (which survives one release as a hidden deprecated
alias). Nothing here writes anything new: every gesture maps to an
existing mutator.**

> **The authoritative epic lives in orun-cloud**
> (`specs/epics/orun-initiatives/`: README, design, api-and-mcp — cluster
> **IN**, milestones IN0→IN6). This folder is the orun half: what this
> repo owns and must hold true. The wire contract is
> `specs/epics/orun-initiatives/api-and-mcp.md` (§1 response shapes, §4
> MCP roster, §5 CLI surface); field-level truth is
> `packages/contracts/src/work.ts`.

## Status

| Field | Value |
|-------|-------|
| Status | **In progress — IN5+IN6 orun legs** (wire client for the four reads + initiative create + milestone upsert · MCP roster 15 → 21 · `orun initiatives` group · `orun work` hidden deprecated alias · docs site renamed) |
| Builds on | `specs/orun-work/` (v2: the fold, the two-log model, import, MCP), `specs/orun-work-v4/` (the hierarchy: intent ladder, sealed briefs, milestones, designs) |
| Coordinates with | orun-cloud `specs/epics/orun-initiatives/` (authoritative; the four read endpoints IN1–IN2, console IN3–IN4) |
| Wire | `/v1/organizations/{org}/work/*` — unchanged prefix, additive reads only (IN-A) |
| Milestone prefix | **IN** (this repo's legs land inside IN5/IN6) |

## What this repo owns

1. **The wire client grows, reads only** (`internal/remotestate/work.go`).
   Four derived reads mirroring the cloud contracts:
   `ListInitiatives` (portfolio: fold-stats, needs-you reasons, epic and
   design rows), `GetInitiativeTree` (the full hierarchy — epics with
   intent, milestones with derived state, tasks with rungs and evidence,
   docs, designs), `GetTaskDetail` (rung with evidence, ancestry,
   components affected, activity tail), `GetWorkActivity` (the tagged
   two-log tail, ancestry-scoped, cursor-paged). Plus the two envelope
   writes the surface needed on this seam: `CreateInitiative`
   (POST /work/initiatives — the route that already existed) and
   `UpsertMilestones` (POST /work/epics/{epic}/milestones, one ladder edit
   per call). `WorkSummary` learns the summary's `initiatives` array.
   Nothing here can carry a status: every new struct is a fold projection.
2. **The MCP grows 15 → 21, the guardrails extend** (`internal/workmcp`).
   New reads: `initiatives_list`, `initiative_tree`, `task_get`,
   `activity_get`. New writes: `initiative_create`, `milestone_upsert`.
   Legacy tool names all stay registered (IN-1 compatibility ledger).
   **Still absent, on purpose:** no approve, adopt, supersede, or pin
   tool — the forbidden-name sweep extends over the new roster, and when
   a write brushes a human-only decision the cloud's typed
   `WorkError("human_only", …)` verdict surfaces verbatim (code included)
   so a model can tell "not allowed for you" from "does not exist" (IN-4).
   Asserted by test.
3. **CLI: `orun initiatives` replaces `orun work`** (`cmd/orun/initiatives.go`).
   The group per api-and-mcp.md §5: `list` (portfolio table), `view`
   (the tree as an indented ladder: intent chips with @revision and named
   drift, milestone states, rung words with evidence hints), `create`
   (`--title`, repeatable `--why` → success criteria), `edit`/`cancel`
   (the item mutators, unchanged), `import` (the v2 importer, unchanged
   wire), `task view`/`task create`, `activity`, `doc pull`, and
   `design list`/`design propose`. Every subcommand keeps `--workspace`,
   `--backend-url`, `--json` (pretty-encoded response structs).
4. **The deprecation of `orun work`** (`cmd/orun/work.go`). The group
   survives exactly one release as a hidden, `Deprecated:`-marked alias
   whose subcommands (`import`, `list`, `edit`, `cancel`) forward to the
   same run functions the new group registers — zero duplicated logic,
   nothing to drift. `orun spec pull` and `orun epic pull` are untouched
   (they already speak the plane's nouns).
5. **Docs.** `website/docs/cli/orun-initiatives.md` replaces
   `cli/orun-work.md` (redirect stub kept), sidebar renamed in place,
   cross-links in the spec/epic/mcp pages updated.

## Invariants this repo enforces (beyond v2/v4's, which all stand)

1. The delivery fold and `internal/worklens` (the conformance oracle) are
   untouched — the new surface renders folds, it never re-derives them.
2. No new read gains a write shadow: the portfolio/tree/detail/activity
   structs have no mutator, and the client offers no call that could
   store a status, a progress number, or a health word.
3. MCP tool names never change once shipped; growth is additive and the
   forbidden-fragment sweep (`status`, `pin`, `lifecycle`, `approve`,
   `adopt`) runs over the whole 21-tool roster.
4. `human_only` refusals pass through the MCP layer verbatim — tests pin
   the code and the server sentence in the isError result.
5. `orun spec pull` and `orun epic pull` behave byte-identically before
   and after this epic.

## Read order

1. orun-cloud `specs/epics/orun-initiatives/README.md` — the one-name
   decision, invariants, milestones IN0→IN6.
2. orun-cloud `specs/epics/orun-initiatives/api-and-mcp.md` — the wire
   contract this repo implements against (§1 reads, §4 MCP, §5 CLI).
3. This file — the orun-half ownership and its enforcement points.
4. `specs/orun-work/` and `specs/orun-work-v4/` — the substrate this
   never breaks.
