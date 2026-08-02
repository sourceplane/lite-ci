---
title: orun integrations
---

`orun integrations` is the console-parity CLI for integration connections —
list what a workspace has connected, check a provider's status, author
integration-bound secrets, and manage scope templates. The verb tree is
**registry-served**: providers, verbs, and help come from the workspace's own
Integration Registry, so the CLI always matches what the platform actually
offers.

The tree is **value-less by design**: no secret value is ever read, sent, or
printed by any of these commands. The ownership boundary with
[`orun secrets`](./orun-secrets.md): `orun secrets` views and manages **all**
secrets and creates static ones; **authoring** an integration-bound secret
(minted from a connected provider) lives here, under the integration
namespace.

## Usage

```bash
orun integrations list [workspace]
orun integrations <provider> status
orun integrations <provider> secret create <KEY> --connection <int_…> --template <id> [--mode brokered|rotated]
orun integrations <provider> templates list
orun integrations <provider> templates create <ID> --base <id> --name <s> [--description <s>]
orun integrations <provider> templates retire|reactivate <ID>
orun integrations sync
```

Persistent flags: `--backend-url`, `--workspace <ws-id|slug>` (`--org` is the
legacy alias). Rung flags on secret-touching verbs: `--env <slug>`,
`--project`, `--shared`. `--json` on every leaf (metadata only).

Command grammar is validated **before** auth or network — a typo never
round-trips, and every unknown provider, resource, or verb gets a
"did you mean" suggestion.

## `list [workspace]`

Lists the workspace's connections, one row per connection:

```
PROVIDER    CONNECTION  ACCOUNT        STATUS  SHARING              CONNECTED
cloudflare  int_0123    Acme Cloud     active  granted (inherited)  2026-07-02
supabase    int_0456    acme-platform  active  auto                 2026-07-14
```

The optional positional selects the workspace — a `ws_…` id or a slug —
and is equivalent to `--workspace`:

```bash
orun integrations list ws_7f3a
orun integrations list acme        # slug works too
orun integrations list --workspace acme
```

Working in one workspace all day? Select it once with
[`orun workspace use <ws>`](./orun-workspace.md) and drop the flag entirely.

Passing both `--workspace X` and a different positional errors
(`pass one`); a workspace that doesn't resolve gets a self-explaining error
showing what was tried and where it came from, how to see your workspaces
(`orun workspace list`), and how to target another one.

## `<provider> status`

```bash
orun integrations cloudflare status
# ● int_0123 — Acme Cloud · active · workspace
#   5 scope template(s) (4 active, 1 custom)
```

## `<provider> secret create`

Creates an integration-bound secret — the value is minted by the platform
from the connection's parent credential; nothing secret touches the CLI:

```bash
orun integrations cloudflare secret create CF_API_TOKEN \
  --connection int_0123 --template workers-deploy --mode rotated \
  --rotation 30d --grace-seconds 3600 --deliver-target cloudflare-worker --env prod
```

| Flag | Meaning |
|---|---|
| `--connection <int_…>` | The connection the value is minted against (required). |
| `--template <id>` | A scope template the provider declares (required — an unknown id errors listing what *is* declared). |
| `--mode` | `brokered` (minted at resolve, never stored — the default) or `rotated` (stored, re-minted on schedule). |
| `--param key=value` | Template parameter (repeatable). |
| `--rotation <cadence>` | Rotation cadence for `--mode rotated` (e.g. `30d`). |
| `--grace-seconds <n>` | Overlap seconds the prior token stays valid (server default 24h). |
| `--deliver-target <t>` | Materialize target re-delivered on rotation. |
| `--display-name <s>` | Human display name for the key. |

## `<provider> templates`

Scope templates are the mint grammar — what a brokered or rotated secret is
allowed to be. `templates list` shows the provider's declared catalog plus
your org's custom templates:

```
ID              NAME                    ORIGIN    STATUS  PARAMS  BASE
workers-deploy  Deploy Workers          declared  active  1       —
deploy-prod     Deploy prod workers     custom    active  1       workers-deploy
```

Author a custom template from a declared base, and retire or reactivate it:

```bash
orun integrations cloudflare templates create deploy-prod \
  --base workers-deploy --name "Deploy prod workers"
orun integrations cloudflare templates retire deploy-prod
orun integrations cloudflare templates reactivate deploy-prod
```

## `sync` — the registry cache

`orun integrations sync` fetches the workspace's Integration Registry and
caches it under `.orun/integrations/`, so provider namespaces, verbs, help
text, and shell completion render offline. The cache is presentation-only —
every invocation is still validated server-side — and goes soft-stale after
24h. `sync` forces a refresh with ETag revalidation (`(registry unchanged)`
when nothing moved).

With a cache present, known providers mount as real subcommand trees; an
unknown provider gets a typo suggestion. Without a cache, bare
`orun integrations` prints help and a sync hint.

## See also

- [`orun secrets`](./orun-secrets.md) — the general secrets surface
- [Secrets](../concepts/secrets.md) — references, resolve postures, output secrets
- [`orun cloud`](./orun-cloud.md) — workspace linking
