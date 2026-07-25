# orun-scope-engine — design

The CLI half of `orun-cloud/specs/epics/saas-scope-engine/`. Normative for the
parser contract, the shared vector file, and the expand/plan integration.
Everything asserted about today's behaviour is read from the tree and cited.

## 1. `internal/scoperef`

Replaces `internal/secretref` (kept as a thin alias for one release so external
callers and the plan reader do not break).

```go
// Scheme is the value plane a reference addresses.
type Scheme string
const (
    SchemeSecret Scheme = "secret"
    SchemeConfig Scheme = "config"
    SchemeFlag   Scheme = "flag"
)

// Ref is a parsed scope reference. A zero-value dimension means "bind to the
// run context at expand time" — NOT a wildcard, and never a widening.
type Ref struct {
    Scheme    Scheme
    Workspace string // "" ⇒ contextual
    Project   string // "" ⇒ contextual
    Env       string // "" ⇒ contextual
    Component string // "" ⇒ contextual
    Key       string
    Version   int    // 0 ⇒ head-at-resolve-time
}

func Parse(s string) (Ref, error)   // strict; never echoes the input
func (r Ref) String() string        // canonical: dimensions in registry order
func IsRef(s string) bool           // the leak guard — scheme prefix only
```

Two properties carry over unchanged from `secretref` and must not regress:

- **`Parse` never echoes its input in an error.** A failing value in a
  secret-shaped slot may *be* a pasted secret, and errors reach logs and CI
  output (`internal/secretref/secretref.go:48-52`). Callers identify the slot by
  key, which they know.
- **`IsRef` is the leak-guard primitive.** Anything in a `secretEnv` slot that
  is not `IsRef` is a literal and a compile error
  (`internal/expand/expander.go:394`).

### Parse rules

| Input shape | Meaning |
|---|---|
| trailing segment | always the KEY (`^[A-Za-z][A-Za-z0-9._-]{0,127}$`, no `:`) |
| `dim:value` | a typed dimension; values match the slug regex, so `:` is a safe delimiter |
| exactly one bare leading segment | workspace |
| exactly four bare segments | the deprecated positional form, normalized on read |
| any other bare arrangement | parse error |
| unknown `dim` | **parse error** — never ignored |
| `@N` | version pin, `N ≥ 1` |

Canonical render order is the registry order (`workspace`, `project`, `env`,
`component`), so a ref's string form is stable — load-bearing, because refs live
in content-addressed plan objects.

### The vector file

`internal/scoperef/testdata/refs.vectors.json` is a **copy of, and CI-checked
against**, `packages/contracts`' fixture in orun-cloud. Shape:

```jsonc
{
  "accept": [ { "in": "secret://acme/api/prod/DB", "ref": { … }, "canonical": "secret://acme/project:api/env:prod/DB" } ],
  "reject": [ { "in": "secret://acme/enviroment:prod/DB", "class": "unknown_dimension" } ]
}
```

Both languages assert `parse(in) == ref`, `render(parse(in)) == canonical`, and
that every `reject` entry fails with the stated class. A vector added on either
side fails the other's CI until mirrored — that is the mechanism, not a
convention.

## 2. Contextual binding at expand

Omitted dimensions bind from the component instance being expanded — the
expander already knows all four (`internal/expand/expander.go`, which resolves
`comp.Name`, the environment name, and the intent's
`execution.state.workspace/project`).

```go
// bindContext fills a ref's empty dimensions from the instance. It never
// overrides an authored value, and never widens: a dimension that cannot be
// bound is a compile error for a required slot.
func bindContext(r scoperef.Ref, inst instanceScope) (scoperef.Ref, error)
```

This replaces reference interpolation. `interpolateString`
(`internal/expand/expander.go:306-323`) is left in place for the rest of the
manifest but **deprecated for reference slots**, with a warning that names the
replacement — because today it silently strips unknown `{{…}}` to empty, which
is how the spec's own documented `{{env}}` example
(`specs/orun-secrets/data-model.md:62-83`) collapses to `secret://acme/api//DB`
and fails with "bad env segment".

Merge precedence is unchanged: intent root → environment → component →
subscription (`mergeSecretEnv`, `expander.go:365-404`), and both existing
guards hold — a literal in `secretEnv` is a compile error, and a plaintext `env`
key may not shadow a `secretEnv` key.

## 3. The component dimension

`internal/planner/secret_bindings.go:129` already synthesizes
`secret://<ws>/<project>/<env>/<KEY>` for each composition `secretBinding`. It
gains the component segment when the instance resolves one.

**The segment is an assertion, never a grant.** The server derives component
from the lease-verified `run_jobs` row and rejects a mismatch with a 400
(`saas-scope-engine/design.md` § 3.3). The CLI emits it so that `orun plan` is
readable offline and so drift surfaces as a loud 400 rather than a silent
widening — it does not, and must not, expect the segment to be trusted.

`mergeBindingRefs`' existing conflict rule extends unchanged: a binding and a
`secretEnv` entry that bind the same `asEnv` to different refs is a compile
error (`secret_bindings.go:139`), now compared on **canonical** form so that
`secret://acme/api/prod/K` and `secret://acme/project:api/env:prod/K` are
recognized as the same reference rather than reported as a conflict.

## 4. The config plane

### Where it resolves

`config://` resolves in **expand**, not in the runner. The resolved value
becomes an ordinary `env` entry in the plan and therefore part of the plan
digest.

```
component.yaml            expand                      plan.json
  env:                      resolve config:// ──►       env:
    API_BASE_URL:             through                     API_BASE_URL: "https://…"
      config://API_BASE_URL   internal/configsurface     (a value — content)
  secretEnv:                                            secretRefs:
    DATABASE_URL:           (untouched — refs only) ──►   [{asEnv: DATABASE_URL,
      secret://DATABASE_URL                                 ref: "secret://…"}]
```

Three consequences, all intended:

- `orun plan` shows exactly what a run will see, offline.
- The run is reproducible from its digest.
- **A config change rescopes its consumers under `--changed`.** This is the
  governance consequence to test for explicitly, not a side effect to discover.

### Failure modes

| Situation | Behaviour |
|---|---|
| `config://` ref unresolvable | compile error in `orun plan`, naming the key and the scope chain walked |
| no backend configured | compile error naming `ORUN_CONFIG_<KEY>` as the local override, mirroring the secrets local-resolver posture (`cmd/orun/run_secrets.go:98-123`) |
| a `masked`-visibility value | resolves normally; redacted in plan *rendering*, present in the digest |
| `secret://` in an `env:` slot | compile error — the existing shadow guard, restated for the new slot |
| `config://` in a `secretEnv:` slot | compile error — `secretEnv` accepts `secret://` only |

### `orun config`

Read-only in this spec (authoring is the console's job, SE9):

```
orun config list [--env <e>] [--chain]      # keys with servesFrom
orun config get <KEY> [--env <e>]           # value + servesFrom + version
orun config diff --env a --env b            # what differs between two envs
```

## 5. `flag://`

Parses, canonicalizes, and is rejected from every manifest slot. Flags are
evaluated per request inside the running application through the SDK, not baked
into a plan — so they have **no manifest form**, and the parser existing without
a slot is the point: a `flag://` in `env:` gets a clear error naming the SDK
call instead of resolving to something plausible and wrong.

## 6. Two live defects closed en route

- **`ttlSeconds` decoded and ignored.** `ResolvedSecrets.TTLSeconds`
  (`internal/remotestate/client.go:402`) has no reader in the tree — verified,
  the only occurrences are the field and its own doc comment — while
  `specs/orun-secrets/runner-integration.md:37-40` requires the in-memory cache
  to honour it and re-resolve under a fresh lease check. A job running past 300s
  uses values beyond their server-declared lifetime. SG1 lands the re-resolve on
  the same call path the parser work already touches.
- **Spec drift in `runner-integration.md`.** The documented
  `internal/secretresolve` package does not exist (the resolver is inline in
  `cmd/orun/run_secrets.go:26`); the documented `TriggerFacts` and `denials[]`
  wire fields are not sent, because the server derives trigger facts from the
  run row — which is the safer design, so the **spec** is what is stale; and
  `client.go:551-552` states the caller re-claims on `409 lease_lost` when
  `remoteSecretResolver` simply propagates it (fail-closed and correct, but the
  comment overpromises). Reconciled here rather than left to mislead.
