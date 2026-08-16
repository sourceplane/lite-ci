# orun-mcp — Design

> **Post-teardown note.** This spec is an as-built record and names the work
> plane as a dependency. That plane was retired whole by
> [`orun-work-teardown`](https://github.com/sourceplane/orun-cloud/tree/main/specs/epics/orun-work-teardown)
> (WT2 in this repo): `internal/worklens`, `internal/workmcp`,
> `internal/workbrief`, the `orun work` / `orun epic` / `orun initiatives` /
> `orun spec` CLI group and the cockpit's Work lane are gone, and its specs
> are in [`specs/archive/`](../archive/). The MCP composes the pen plane (`internal/penmcp` — `pr_open`) and the
> platform plane; the vendored manifest is 27 tools.

Status: Draft (decisions marked **locked** in `README.md` § Status).

## 1. Product shape

One command, one connection, everything orun:

```
claude mcp add orun -- orun mcp serve
```

```
            orun mcp serve  (stdio · one JSON-RPC loop · serverInfo "orun")
                    │
       ┌────────────┴──────────────┐
  work provider              platform provider
  (internal/workmcp,         (internal/platformmcp, new)
   9 tools, unchanged)        25 tools, native Go
       │                            │
  work-plane routes           public API routes
  /v1/organizations/…/work    /v1/organizations/…  (catalog, runs, audit,
       │                            │              events, usage, billing,
       └──────────┬─────────────────┘              config, webhooks, …)
                  ▼
       internal/remotestate.Client  (bearer via TokenSource: OIDC → ORUN_TOKEN
                                     → OP1 session; refresh, retry, error decode)
```

**Mounting is contextual, never guessed.** At serve start:

- Backend + auth resolve (the `workClient` preamble: `--backend-url` >
  `ORUN_BACKEND_URL` > `intent.yaml` `execution.state.backendUrl` > config
  `cloud.url`; token via `remotestate.ResolveTokenSource`) — required, as
  today; no auth → the existing actionable "run `orun auth login`" error.
- **Work provider** mounts when a workspace scope resolves (flag > env >
  intent > cached repo link) — the current behavior, verbatim.
- **Platform provider** always mounts once auth resolves; its tools take an
  explicit `workspace` argument (parity with the TS plane), with the resolved
  ambient scope applied as a default exactly like MCP1's
  `applyWorkspaceDefault` semantics (explicit input wins; the advertised
  schema marks `workspace` optional when a default is active).

No collision is possible: platform tool names are the shipped 25; work tools
are `work_*`/`spec_*`/`task_*`/`contract_*` — disjoint by `saas-mcp-server`
locked decision 8. A merged-roster test enforces uniqueness anyway.

## 2. Provider composition (UM0)

`internal/workmcp/server.go` currently owns both the stdio JSON-RPC loop and
the work tools. Factor, don't rewrite:

- A `ToolProvider` interface — `Tools() []ToolDef` +
  `Call(ctx, name, args) (Result, bool)` — in a small shared package
  (`internal/mcpserve`), extracted from `workmcp`'s existing `toolDef` /
  dispatch shapes. The 16 MiB scanner loop, `initialize`/`ping`/`tools/list`/
  `tools/call` handling, notification silence, and `-32601` behavior move
  verbatim.
- `mcpserve.Server{Providers: […]}` merges rosters for `tools/list` and
  routes `tools/call` by name. serverInfo becomes `{name: "orun", version:
  <binary version>}` (was `orun-work`/`"1"` — the name was cosmetic; the
  configured client name is what users see, and docs never shipped the old
  serverInfo as a contract).
- `workmcp` keeps its tools and its `WorkAPI` seam; it just implements
  `ToolProvider`. Its forbidden-tool test (no `status`/`pin`/`lifecycle` in
  any name) is promoted to run over the **merged** roster.
- Protocol revision stays the hand-rolled `2024-11-05` loop for now; it is
  what the work MCP already speaks and every mainstream client negotiates
  down. Moving to the official Go SDK is a recorded later option (risks R3),
  not a prerequisite.

## 3. The native platform plane (UM1–UM2)

`internal/platformmcp`, a sibling of `workmcp`, mirroring its shape:

- **Seam**: `PlatformAPI` interface with one method per wrapped endpoint
  family, satisfied by `*remotestate.Client` extended with public-API wire
  methods following `internal/remotestate/work.go`'s exact pattern
  (`doJSON` over `/v1/organizations/{org}/…` paths; the success-envelope
  unwrap, retry, `Retry-After`, and platform error decode come free). Tests
  use a `fakeAPI` like `workmcp`'s.
- **Tools**: the 25 shipped tools, names and input schemas **identical** to
  the TS plane — 19 reads (whoami, workspaces_list, projects_list,
  catalog_search, catalog_get_entity, catalog_read_doc, runs_list, runs_get,
  runs_read_logs, audit_search, events_search, security_events_list,
  access_explain, usage_summary, quota_check, billing_summary, config_read,
  secrets_list [metadata only — no value path exists], webhook_deliveries_list)
  and 6 writes (project_create, environment_create, flag_set, webhook_create,
  webhook_delivery_replay, member_invite). Output discipline matches: JSON
  data + short text summary, cursors passed through, 64 KiB byte-capped
  logs/docs with the explicit truncation marker.
- **Write rails** (UM2): per-attempt `Idempotency-Key` (accepting a supplied
  key ≤ 255 printable ASCII); `x-client-surface: mcp` on **every** platform
  call (reads included) via the client's header seam; `--read-only` filters
  writes out of `tools/list`, not just execution; `member_invite` strips the
  one-time accept token (parity with the TS guard).
- **Entitlement** (UM2): lazy once-per-workspace check of
  `feature.mcp_server` via the public entitlements read, 60s cache,
  fail-open, denial surfaces as the platform's `entitlement_required` error —
  mirroring the TS transports.

## 4. Manifest parity — the anti-drift contract

Two implementations of one tool plane drift unless a machine checks them.
The TS plane (`orun-cloud/packages/mcp`) is the **source of truth**: MCP9
(sibling epic) exports `tool-manifest.json` — for every tool: name, title,
description, JSON-Schema input, annotations (readOnly/destructive/idempotent
hints) — generated from the registry and freshness-tested there.

This repo vendors it at `specs/orun-cloud/vendored/mcp-tool-manifest.json`
(the OC0 vendored-contract lane; CI diffs vendored files). A Go parity test
in `internal/platformmcp` asserts: exact roster, name-for-name schema
equality (normalized JSON-Schema comparison), and annotation equality. A
tool change lands TS-first, re-exports the manifest, re-vendors here, then
the Go side goes green — the same one-way flow as the state wire contract.

**Parity is over the tools this plane advertises, not the manifest's whole
roster.** The TS plane serves some tools this repo already serves natively:
orun-cloud IN9 added four work-plane reads (`initiatives_list`,
`initiative_tree`, `task_get`, `activity_get`) so that hosted agents on the
remote MCP — which have no orun binary, and so no work plane at all — can
reach the work fold. Locally `internal/workmcp` already owns those names.

So the manifest carries 29 tools and `internal/platformmcp` advertises 25
(19 reads + 6 writes), ceding the four via `cededToWorkPlane`. The platform
counts elsewhere in this spec are that advertised 25/19 and did not move
when the manifest grew. Ceding is load-bearing twice over: `checkRoster`
rejects a duplicated name outright (§2), and this plane has no
implementation for those four, so advertising them would promise a tool it
cannot serve. Re-vendor procedure and drift guards:
`specs/orun-cloud/vendored/README.md`.

## 5. What deliberately does not change

- **Work MCP semantics** — WP5's tools, mutator-only writes, sealed briefs,
  and the no-status/no-pin/no-lifecycle invariants are consumed, not edited.
- **The remote server** — `apps/mcp-worker` keeps serving the TS plane over
  Streamable HTTP with OAuth/`sk_` auth; hosted clients are unaffected.
- **The TS local server** — `orun-cloud mcp serve` keeps working as the
  node *reference implementation*; docs stop pointing at it (MCP10).
- **`orun-agents-live` §4.4** — simplifies: the driver config lists **one**
  MCP endpoint (`orun mcp serve`) instead of orun-MCP + platform-MCP; the
  in-sandbox session credential rides `ORUN_TOKEN`. A pointer note lands in
  that spec with UM1.

## 6. CLI surface

```
orun mcp serve   [--read-only] [--workspace <ref>] [--backend-url <url>]
orun mcp tools   [--read-only] [--json]     # merged roster, provider column
```

`mcp tools` is new here (parity with the node CLI's roster listing) and
prints provider (`work` | `platform`), read-only marker, and description.
Serve keeps absolute stdout protocol purity; all diagnostics to stderr.
