# Spec: orun-grounded-sessions — the runtime leg of grounded sessions

**A session starts where the work lives.** The cloud epic
(`orun-cloud/specs/epics/saas-grounded-sessions/`) lets an operator pick a
linked repository in the composer and binds the choice to the session; this
spec is the runtime half: `orun agent serve` turns the binding into a real
checkout (clone at the right ref, task-keyed branch, driver workdir inside
it), a **git credential helper** keeps git authenticated with short-lived
repo-scoped tokens minted through the session's own credential, and the
rotating session token becomes available to every `orun` invocation in the
sandbox via a token file — so the sandbox is a first-class client of its
workspace and killing the session severs everything at once.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft** — authored with the cloud epic; the canonical model (env contract, token door, tiers, kill story) lives in the cloud `design.md` and is vendored here only where the runtime enforces it |
| Cluster | **GS** (grounded sessions — cross-repo; orun owns **GS0–GS2**, orun-cloud owns **GS3–GS8**) |
| Target branch | `main` (PRs merged incrementally) |
| Builds on | `orun-agents` AG0–AG4 (the loop, `RunOptions.Workdir`/`Branch`, the driver brief); `orun-agents-live` AL4/AL8 as-built (`command_agent_serve.go` — env identity, heartbeat-first boot, the relay base URL); `internal/agent/attach` (the session bearer + rotation the helper and token file ride); `internal/remotestate` (`ResolveAuth` — the precedence chain GS2 extends); cloud `saas-integrations` IG4 / `saas-integration-tenancy` IT4 (the broker the door fronts) |
| Decisions locked | (1) **Grounding lives in serve, never in bash** — AL8 made `exec orun agent serve` the whole entrypoint; the clone, branch, and credential setup are Go code inside the session's event stream, and a grounding failure fails the session loudly. (2) **Pull, not bake** — no git credential arrives in env; the helper mints ≤1h repo-scoped tokens through `POST {relay base}/repo-token` with the session bearer, per git operation, with an expiry-aware 0600 cache. (3) **One credential for the workspace** — no second token: serve writes each session-token rotation to `ORUN_TOKEN_FILE` (0600) and `ResolveAuth` learns the file source (directly after the `ORUN_TOKEN` env check), so every in-sandbox `orun` verb authenticates as the session, fresh, lease-gated. (4) **The checkout may survive a snapshot; no credential may** — resume re-mints before the first git operation, same path as boot. |
| Gate | Human-independent: GS0–GS2 develop against a fake relay serving the GS4 wire shape (the AL fixture pattern). Live verification rides the cloud GS8 smoke. |

## What changes in the sandbox

| Today | After GS0–GS2 |
|---|---|
| `orun agent serve` boots into an empty `$HOME`; the driver's workdir is wherever serve ran | `ORUN_REPO_*` env ⇒ clone at `ORUN_REPO_REF`, branch `agent/<task>-<type>` created, driver brief workdir inside the checkout |
| No git credential exists; `git fetch` cannot work at all | `orun git-credential` answers git's credential protocol with a freshly minted, repo-scoped, ≤1h token; the harness env gets `GITHUB_TOKEN` seeded at driver launch |
| Only the agent relay answers to the sandbox; `orun cloud check` cannot authenticate | `ORUN_TOKEN_FILE` + `ORUN_WORKSPACE` ⇒ `orun cloud check`, state/catalog reads, and policy-gated secret resolves all work as the session principal |

## Milestones at a glance

| ID | Milestone | Status |
|----|-----------|--------|
| GS0 | **Grounded serve**: read `ORUN_REPO_*`; mint a read token; clone (blobless partial, full history) at `ORUN_REPO_REF`; create the task-keyed branch; hand the workdir to the driver brief; clone failure ⇒ loud terminal session failure | 🗓️ Planned |
| GS1 | **`orun git credential`**: the credential-helper subcommand (protocol `get`; `store`/`erase` no-ops) over the repo-token door; expiry-aware 0600 cache; helper wired via repo-local git config; `GITHUB_TOKEN` seeded into the harness env at driver launch | 🗓️ Planned |
| GS2 | **Workspace-client auth**: serve writes rotations to `ORUN_TOKEN_FILE`; `remotestate.ResolveAuth` file source; `ORUN_WORKSPACE` honored end-to-end; `orun cloud check` green in-sandbox | 🗓️ Planned |

## Read order

1. This README.
2. The cloud epic's [`design.md`](../../../orun-cloud/specs/epics/saas-grounded-sessions/design.md)
   — canonical for the env contract (§3), the repo-token door (§4), tiers
   (§5), workspace planes (§7), the kill story (§9), and the snapshot rule
   (§10).
3. [`design.md`](./design.md) — the runtime specifics: serve's grounding
   sequence, the credential helper, the token file, and the failure taxonomy.
4. [`implementation-plan.md`](./implementation-plan.md) — GS0–GS2 with
   "done when".
