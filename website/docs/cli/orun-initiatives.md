---
title: orun initiatives
---

`orun initiatives` is the CLI face of the **work plane** — orun's
delivery-derived work tracker (specs/orun-initiatives, on the orun-work
substrate). Its central invariant: **lifecycle is a derived query, not a
stored status**. A task's rung (Draft → Ready → In Progress → In Review →
Done → Released) is computed by folding two append-only logs — the
coordination log (human/agent events) and the observation log (facts the
platform observed: branches, PRs, merges, gate verdicts) — so nobody, human
or agent, can *set* a status anywhere. There is no `set-status` subcommand,
deliberately and permanently.

```bash
orun initiatives <subcommand> [flags]
```

Requires a linked workspace (see [`orun cloud`](./orun-cloud.md)) or explicit
`--workspace` / `--backend-url` flags. Every subcommand accepts `--json`
(pretty-encoded response structs) for scripting.

:::note
`orun initiatives` replaces `orun work`. The old group survives one release
as a hidden alias that forwards to the same implementations, then it is
removed.
:::

## Subcommands

| Subcommand | Purpose |
| --- | --- |
| `list` | The portfolio: every initiative with derived status, progress, needs-you |
| `view <key>` | One initiative's tree, rendered as an indented ladder |
| `create` | Create an initiative envelope (`--title`, repeatable `--why`) |
| `edit <key>` | Edit an item's envelope (title/description/owner/target/…) |
| `cancel <key>` | Retire a task or epic — the append-only "delete" |
| `import <specs-dir>` | Map a `specs/` tree to the hierarchy and apply to the workspace |
| `task view <key>` | Task detail: rung ladder, evidence, components, activity |
| `task create` | Create a task (`--epic`, `--milestone`, contract flags) |
| `activity <key>` | The tagged activity tail for any noun |
| `doc pull <key>` | Print an epic spec / design doc (markdown) to stdout |
| `design list <ini>` | List an initiative's design runs |
| `design propose <ini>` | Start a Draft design run |

## `orun initiatives list`

```bash
orun initiatives list --workspace my-org
```

Prints the portfolio table — `KEY · TITLE · STATUS · PROGRESS · NEEDS YOU ·
TARGET` — plus the workspace fold-stats footer. Everything is derived on
read: **status** is `planning` until the initiative's first approved epic
and the v4 health fold (`on_track` / `at_risk` / `off_track`) afterwards;
**progress** is `done/total` over non-canceled member tasks; **needs you**
names the first reason the initiative waits on a human (approval drifted,
awaiting approval, review open, milestone idle, design in review).

## `orun initiatives view`

```bash
orun initiatives view my-initiative
```

Renders one initiative's whole tree as an indented ladder:

- the initiative header — status, owner, target, progress, needs-you lines;
- each **epic** with its intent chip (`intent approved @b2d4aa00`, drift
  named when the doc or ladder moved since approval) and progress;
- each **milestone** with derived state (`complete` / `active` /
  `upcoming` — pure ladder arithmetic) and `done/total`;
- each **task** with its rung word and the evidence that put it there
  (`branch …; PR #9 open, checks 3/4`), plus a `backlog:` section for
  tasks outside any milestone;
- the epic's documents (spec + designs, with revision and open threads)
  and the initiative's design runs.

## `orun initiatives create`

```bash
orun initiatives create --title "Payments v2" \
  --why "checkout conversion +2pt" --why "PCI scope shrinks" \
  --owner usr_ab12 --target 2026-12-01
```

Creates the strategic envelope. `--why` is repeatable — each occurrence
becomes one success criterion. `--slug` defaults to the lowercased,
hyphenated title. An initiative has no lifecycle and no contract: its
status, progress, and health derive from its member epics' logs.

## `orun initiatives edit` / `orun initiatives cancel`

```bash
orun initiatives edit my-epic --target 2026-10-01 --initiative payments-v2
orun initiatives cancel WRK-12 --yes
```

`edit` sends only the flags you pass through the one `item_edited` mutator
(pass an empty value to clear a field). `cancel` folds a terminal
`canceled` state onto a task or epic — attributed, append-only, permanent;
initiatives have no lifecycle to cancel.

## `orun initiatives import`

Parses a spec tree into a deterministic import plan for the planning
hierarchy:

| In the repo | Becomes |
| --- | --- |
| Epic folder's `README.md` | An **Epic** with a content-addressed doc digest |
| `implementation-plan.md` `## <KEY> — <Title>` headings | **Milestones** on that epic |
| Checklist items under a heading | **Tasks** inside that milestone |
| `roadmap.md` cluster rows | **Initiatives** grouping the epics |

```bash
orun initiatives import specs/ --dry-run          # print the plan, change nothing
orun initiatives import specs/ --workspace my-org # apply (idempotent re-runs)
orun initiatives import specs/ --prefix PAY       # task-key prefix (default WRK)
```

Import writes intent, never decisions: no lifecycle, reviews, or approvals
cross the wire, and re-imports are idempotent (created entities carry their
import provenance). Pre-v4 corpora migrate key-preservingly into the newly
minted milestones.

## `orun initiatives task`

```bash
orun initiatives task view WRK-42
orun initiatives task create --epic my-epic --milestone M2 \
  --title "wire the reader" --contract-done-when "GET returns the fold" \
  --contract-done-when "tests pass"
```

`task view` prints the whole task page: the rung ladder with the current
rung bracketed, ancestry (initiative/epic/milestone), the folded delivery
evidence (branch, PR with checks, gate results), components affected
(observation diffstats — empty when the world reported none, never
invented), and the task-scoped activity tail. `task create` authors the
envelope and contract (`--contract-goal`, repeatable `--contract-done-when`
/ `--contract-affects` / `--contract-gate` / `--contract-dep`); the rung
derives from observations afterwards.

## `orun initiatives activity`

```bash
orun initiatives activity my-epic --limit 100
```

The tagged tail: both logs folded into one reverse-chronological list of
`TIME  TEXT  [TAG]` lines. The tag trail is ancestry — filtering by an epic
covers its milestones' tasks, docs, and designs; an initiative covers its
whole subtree. Page with `--cursor`.

## `orun initiatives doc pull`

```bash
orun initiatives doc pull my-epic > SPEC.md
orun initiatives doc pull my-epic --rev sha256:b2d4…
```

Prints an item's content-addressed cloud document (an epic's spec or a
design's doc) as markdown on stdout — latest revision unless `--rev` pins
one. For the *approval-sealed, verified* brief, use
[`orun epic pull`](./orun-epic.md).

## `orun initiatives design`

```bash
orun initiatives design list payments-v2
orun initiatives design propose payments-v2 --title "Split the ledger" \
  --doc-ref sha256:ab12… --proposal '{"epics":[…]}'
```

A design is a **proposal** — humans review, compare, and adopt; adoption
mints the epics and stays human-only (there is no adopt subcommand, and the
cloud refuses agent adoption with a typed `human_only` verdict).

## Related

- [`orun spec`](./orun-spec.md) — frozen, content-addressed spec briefs
- [`orun epic`](./orun-epic.md) — the approval-sealed epic brief (v4)
- [`orun mcp`](./orun-mcp.md) — the agent tool surface over the same fold
