# orun-grounded-sessions — Design (runtime leg)

Status: Draft. The canonical model — env contract, token door wire shape,
access tiers, kill story, snapshot rule — lives in the cloud epic's
`design.md` (`orun-cloud/specs/epics/saas-grounded-sessions/`). This doc
specifies only what the orun binary does with it.

## 1. Serve's grounding sequence (GS0)

Grounding slots into `runAgentServe` after the heartbeat and relay are up
(liveness first — a grounding failure must be *reportable*) and before the
agent type/brief/driver assembly (the driver must see the final workdir):

```
banner → identity check → heartbeat → relay → [GROUNDING] → store → type → brief → driver → Run
```

Steps, all inside the session's event stream:

1. **Detect**: `ORUN_REPO_REMOTE` present ⇒ the session is grounded. Absent ⇒
   the entire feature is inert (today's behavior, byte-identical).
2. **Prepare**: workdir root `$HOME/work/<repo-name>` (repo name from
   `ORUN_REPO_FULL_NAME`); refuse to reuse a non-empty dir that is not a git
   repo for the same remote (resume safety, cloud design §10).
3. **Credential wiring before first contact**: write repo-local git config on
   the clone via `git clone --config credential.helper='!orun git-credential'`
   — helper config rides the clone command itself, so no global git state and
   no window where a fetch could prompt.
4. **Clone**: `git clone --filter=blob:none` (full history, blobs on demand —
   agents want `git log`/`blame`; blobless keeps boot fast on big repos) at
   `--branch ORUN_REPO_REF`. On resume (dir already a valid clone of the same
   remote): `git fetch` + reuse.
5. **Branch**: create/reuse `agent/<task>-<type>` (the exact string
   `command_agent_serve.go` already computes) from `ORUN_REPO_REF`.
6. **Hand off**: `RunOptions.Workdir` = the checkout; `Brief.Workdir` follows
   (already plumbed — `cmd.Dir` for the harness).
7. **Failure**: any step fails ⇒ log the specific stage on stderr and the
   event stream, then exit non-zero with a distinct message
   (`repo_clone_failed: <stage>: <cause>`) — the cloud sweep/failure path
   turns that into `failed` with the error visible on the session. A grounded
   session without its repo must never silently degrade to an ungrounded one.

Egress note: the clone URL is always `https://github.com/...` — already on the
sandbox allowlist; grounding adds no hosts.

## 2. `orun git-credential` (GS1)

A hidden top-level subcommand (`orun git-credential <get|store|erase>`)
implementing git's credential-helper protocol.

- **`get`**: parse `protocol`/`host`/`path` from stdin; serve only the bound
  repo (host `github.com`, path matching `ORUN_REPO_FULL_NAME`) — anything
  else prints nothing (git falls through to other helpers, i.e. fails clean).
  Mint via `POST {ORUN_CLOUD_API}/v1/organizations/{ORUN_ORG_ID}/agents/sessions/{ORUN_SESSION_ID}/repo-token`
  with the session bearer (env `ORUN_SESSION_TOKEN`, else `ORUN_TOKEN_FILE`).
  Print `username=x-access-token` / `password=<token>`.
- **Cache**: `$XDG_RUNTIME_DIR/orun/repo-token.json` (0600; fallback under
  `os.TempDir()` per-uid), holding `{token, expiresAt}`; reused until
  `expiresAt − 5m`. One flight at a time (file lock — the `cliauth`
  refresh-lock pattern) so parallel git subprocesses don't stampede the door.
- **`store` / `erase`**: accepted, ignored (`store` must not persist anything;
  `erase` clears the cache file — the one honored side effect).
- **Errors**: a 403 from the door prints nothing and logs once (the tier was
  lowered or the session is done — git reports auth failure honestly); a 502
  retries with backoff (3×) before giving up, matching the relay's posture.
- **Harness seeding**: at driver launch, serve mints once and sets
  `GITHUB_TOKEN` in the harness env (compatibility for `gh`/API tools). Git
  itself never reads it — the helper is authoritative. Documented staleness:
  the env token dies ≤1h; long-session API needs are a future
  `orun integrations github token` verb over the same door, not a longer TTL.

## 3. The token file (GS2)

- Serve already rotates the session token (heartbeat-driven). GS2 writes each
  rotation to `ORUN_TOKEN_FILE` (default `$XDG_RUNTIME_DIR/orun/session-token`,
  0600, atomic rename) and exports `ORUN_TOKEN_FILE` + `ORUN_WORKSPACE` (=
  `ORUN_ORG_ID`) into the harness env.
- `remotestate.ResolveAuth` gains the file source, placed **directly after**
  the `ORUN_TOKEN` env check and before OIDC/session: read at call time (never
  cached across calls — the file is the rotation carrier), trimmed, refused if
  the file is group/other-readable (the `cliauth` storage discipline).
- Result: any `orun` verb a tool call spawns inside the sandbox — `cloud
  check`, `state`, `secrets resolve`, the work MCP — authenticates as the
  session principal with the freshest token, and dies with the lease.

## 4. What GS0–GS2 must not do

- No global git config mutation; no credential in URLs, `.git/config` remotes,
  or env consumed by git.
- No new attach frames, event kinds, or seal-vocabulary growth — grounding
  progress rides existing log/status events.
- No write-plane access: the session principal's cloud grants (GS7) are
  read + policy-gated resolve; the runtime must not assume more.
- No repo other than the bound one — the helper's path filter and the door's
  binding check enforce it from both sides.
