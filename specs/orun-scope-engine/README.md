# Spec: orun-scope-engine — one reference grammar, two resolved planes

**The binary already carries a secret-reference grammar; it should carry a
*scope*-reference grammar.** Today `internal/secretref` implements a fixed
positional 4-tuple (`secret://<workspace>/<project>/<env>/<KEY>[@<version>]`)
that cannot gain a dimension without rewriting every reference string in an
immutable, content-addressed object graph — and whose documented `{{env}}`
interpolation form does not actually work. This spec generalizes it to typed
dimension segments with contextual omission, adds the **component** dimension
the platform is growing, and lands the second resolved plane: **config**
references that resolve at *plan time* and become part of the plan digest.

Platform half: `orun-cloud/specs/epics/saas-scope-engine/` (cluster **SE**).

## Status

| Field | Value |
|-------|-------|
| Status | **Draft (SG0–SG4) — for review; pairs SE3/SE4/SE6** |
| Cluster | **SG** (scope grammar — pairs `orun-cloud` epic **SE**) |
| Owner(s) | `internal/secretref` → `internal/scoperef` · `internal/expand` · `internal/planner` · `internal/render` · `internal/runner` · `cmd/orun` |
| Target branch | `main` |
| Builds on | the shipped secrets plane (`internal/secretref`, `internal/planner/secret_bindings.go`, `internal/runner` resolve + redact + materialize, `cmd/orun/run_secrets.go`) · `internal/remotestate` auth + lease plumbing · `internal/expand` 4-layer `secretEnv` merge |
| Decisions locked | (1) **One grammar, three schemes** — `secret://` · `config://` · `flag://` share a parser; `flag://` parses but has no manifest slot (flags are runtime, in-app). (2) **Omission binds to run context** — this replaces reference interpolation entirely. (3) **The shared test-vector file is normative**; the Go parser and `packages/contracts` are driven from the same fixtures. (4) **`env:` accepts a `config://` ref**; no new manifest block. (5) **The component segment is an assertion, never a grant** — the server derives component from the lease and rejects a mismatch. |

## Why the grammar has to change

Three defects, all live:

1. **It cannot grow.** Positional tuples have no room for a fifth dimension.
   Adding one rewrites every ref in the content-addressed graph — the operative
   argument, not aesthetics.
2. **The documented interpolation form is broken.**
   `specs/orun-secrets/data-model.md:62-83` documents
   `secret://acme/api/{{env}}/DATABASE_URL`, but `interpolateString`
   (`internal/expand/expander.go:306-323`) only substitutes `{{.environment}}`,
   `{{.group}}`, `{{.component}}` and **strips** anything else to empty — so the
   documented example collapses to `secret://acme/api//DATABASE_URL` and fails
   `secretref.Parse` with "bad env segment".
3. **The grammar is duplicated by hand.** `internal/secretref/secretref.go:23-25`
   and `apps/state-worker/src/handlers/secrets-resolve.ts:49-50` are two regexes
   kept in sync by discipline. Named dimensions make the parser meaningfully
   more complex; a third copy in the console is how you ship a UI that renders
   references the CLI rejects.

## The grammar

```
<scheme>://[<workspace>/][<dim>:<value>/]*<KEY>[@<version>]
```

```yaml
# component.yaml — portable across every environment, no templating
spec:
  env:
    LOG_LEVEL:    info
    API_BASE_URL: "config://API_BASE_URL"        # resolved at PLAN time → digest
  secretEnv:
    DATABASE_URL: "secret://DATABASE_URL"        # resolved at RUN time → job env
    STRIPE_KEY:   "secret://env:prod/STRIPE_KEY" # pin one axis, inherit the rest
```

- Last segment is always the KEY (KEYs cannot contain `:`).
- A segment with `:` is `dim:value`; one leading bare segment is the workspace;
  exactly four bare segments is the deprecated positional form, normalized on
  read.
- **Unknown dimension ⇒ parse error.** Silently dropping `enviroment:prod` would
  resolve at a *broader* scope than the author meant.
- Omission binds to the run context, so one manifest works in every environment.

## The two planes, and why they resolve at different times

**Secrets are carved *out* of the content-addressed graph because a value can
never be content. Config belongs *in* it because it should be.**

| | `secret://` | `config://` |
|---|---|---|
| Resolves in | `internal/runner`, per job, lease-bound | `internal/expand`, at plan time |
| Enters plan digest | never (refs only) | **yes — the value is content** |
| Manifest slot | `secretEnv:` | `env:` |
| Failure mode | fail-closed before step 1 | compile error in `orun plan` |
| Redacted | always | per served `visibility` |

Config in the digest means `orun plan` shows exactly what a run will see,
offline; the run is reproducible from its digest; and **a config change rescopes
its consumers under `--changed`**. That last one is intended and is the visible
consequence to test for.

## Milestones

| ID | Milestone | Pairs |
|----|-----------|-------|
| SG0 | Spec (this) | SE0 |
| SG1 | **`internal/scoperef`** — the generalized parser/renderer driven by the shared vector file; `internal/secretref` kept as a thin alias for one release. No caller behaviour change. | SE4 |
| SG2 | **Component dimension** — `component:` parses, renders, and canonicalizes; `internal/planner/secret_bindings.go` emits it when the instance resolves one; plan `secretRefs` round-trip. | SE3 |
| SG3 | **Contextual omission** — omitted dimensions bind at expand from the component instance's (workspace, project, env, component); reference-slot `{{…}}` interpolation deprecated with a loud warning naming the replacement. | SE4 |
| SG4 | **The config plane** — `config://` accepted in `env:`, resolved at plan time through `internal/configsurface`, rendered in `orun plan` with its `servesFrom`, and sealed into the digest. `orun config` read commands. | SE6 |

## Read order

1. `design.md` — the parser contract, the vector file, the expand/plan
   integration, and the migration path for existing refs.
2. `implementation-plan.md` — per-milestone **Build.** / **Done when.**

## Scope boundary

| In scope | Out of scope |
|----------|--------------|
| `internal/scoperef` + shared vectors | The lease-bound resolve wire call (unchanged beyond the parsed shape) |
| The component segment as a plan-time assertion | Deriving or trusting component client-side — the server derives it from the lease |
| `config://` in `env:` and plan-time resolution | A `flag://` manifest slot — flags are runtime, in-app, and deliberately have none |
| Deprecating reference-slot interpolation | Changing `{{.environment}}` interpolation elsewhere in the manifest |
| `orun config` read surface | `orun config` write surface (console owns authoring in SE9) |

## Two live defects this closes, en route

- **`ttlSeconds` is decoded and ignored.** `ResolvedSecrets.TTLSeconds`
  (`internal/remotestate/client.go:402`) has no reader anywhere in the tree —
  the only occurrences are the field and its own doc comment — while
  `specs/orun-secrets/runner-integration.md:37-40` requires the in-memory cache
  to honour it and re-resolve with a fresh lease check. A job running longer
  than 300s uses values past their server-declared lifetime. SG1 lands the
  re-resolve alongside the parser work, since both touch the same call path.
- **Spec drift in `runner-integration.md`.** The documented
  `internal/secretresolve` package does not exist (the resolver is inline in
  `cmd/orun/run_secrets.go`), the documented `TriggerFacts`/`denials[]` wire
  fields are not sent (the server derives trigger facts from the run row, which
  is the safer design), and `client.go:551-552` claims the caller re-claims on
  `409 lease_lost` when it simply propagates. Reconciled here rather than left
  to mislead the next reader.
