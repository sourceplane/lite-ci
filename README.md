# liteci - Schema-Driven Planner Engine

A policy-aware workflow compiler that turns **intent** into executable **plan DAGs**. Built on CNCF principles.

```
Intent.yaml + Jobs.yaml + Schemas
          ↓
    Planner Engine
          ↓
    Plan.json (DAG)
```

## Architecture

### 3-Stage Compiler

1. **Normalize** - Validate and canonicalize intent
2. **Expand** - Environment × Component matrix with policy merging
3. **Plan** - Bind jobs, resolve dependencies, materialize DAG

### Core Principles

- **Intent is policy-aware, not execution-specific**
- **Jobs define HOW, Intent defines WHAT**
- **Plans are deterministic and execution-runtime agnostic**
- **Schema controls everything** - inputs, expansion, validation

## Project Structure

```
liteci/
├── cmd/liteci/
│   └── main.go           # CLI entry point
├── internal/
│   ├── model/            # Pure data structures
│   │   ├── intent.go
│   │   ├── job.go
│   │   └── plan.go
│   ├── loader/           # YAML/Schema loading
│   ├── schema/           # Schema validation
│   ├── normalize/        # Intent canonicalization
│   ├── expand/           # Env × Component expansion
│   ├── planner/          # Job binding & DAG
│   └── render/           # Plan materialization
├── schemas/              # JSON schemas
├── examples/
│   ├── intent.yaml
│   └── jobs.yaml
└── README.md
```

## Getting Started

### Build

```bash
go build -o liteci ./cmd/liteci
```

### Commands

#### 1. Validate Files

```bash
./liteci validate -i examples/intent.yaml -j examples/jobs.yaml
```

Output:
```
✓ Intent is valid
✓ Job registry is valid (4 jobs)
✓ All validation passed
```

#### 2. Debug Intent Processing

```bash
./liteci debug -i examples/intent.yaml -j examples/jobs.yaml
```

Output:
```
Metadata: {Name:microservices-deployment Description:...}
Groups: 2
  - platform: policies=map[isolation:strict ...], defaults=...
  - onboarding: ...
Environments: 3
  - production: 3 components, policies=...
  - staging: 3 components, policies=...
  - development: 4 components, policies=...
Components: 4
  - web-app: type=helm, domain=platform, enabled=true, deps=1
  - web-app-infra: type=terraform, domain=platform, enabled=true, deps=1
  - common-services: type=helmCommon, enabled=true, deps=0
  - component-charts: type=charts, enabled=true, deps=1
```

#### 3. Generate Plan

```bash
./liteci plan -i examples/intent.yaml -j examples/jobs.yaml -o plan.json --debug
```

Output:
```
📋 Loading intent...
📚 Loading job registry...
🔍 Normalizing intent...
📦 Expanding (env × component)...
Generated 11 component instances
🔗 Binding jobs and resolving dependencies...
🔄 Detecting cycles...
📊 Topologically sorting...
Sorted 11 jobs
✨ Rendering plan...

Plan: microservices-deployment (Example multi-environment microservices deployment)
Jobs: 11

Job: web-app@production.deploy
  Component: web-app
  Environment: production
  Type: helm
  Steps: 4
  DependsOn: [common-services@production.deploy]
...

✅ Plan generated with 11 jobs
```

## Intent Schema

### Top-Level Structure

```yaml
apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: deployment-name
  description: Human-readable description
groups:
  <domain-name>:
    policies:       # Cannot be overridden
      key: value
    defaults:       # Can be overridden
      key: value
forEach:            # Environment definitions
  <env-name>:
    selectors:
      components: [list]
      domains: [list]
    defaults: {...}
    policies: {...}
components:
  - name: comp-name
    type: helm|terraform|charts|...
    domain: domain-name
    enabled: true
    inputs: {...}
    labels: {...}
    dependsOn: [...]
```

### Merge Precedence (Lowest → Highest)

```
1. Type defaults
2. Job defaults
3. Group defaults (from domain)
4. Environment defaults
5. Component inputs (highest priority)
```

**Policies**: Cannot be merged, enforced as constraints.

## Job Registry Schema

```yaml
apiVersion: sourceplane.io/v1
kind: JobRegistry
jobs:
  <component-type>:
    name: job-name
    description: Description
    timeout: 15m
    retries: 2
    steps:
      - name: step-name
        run: |
          shell command with {{.Variable}} templates
        timeout: 5m
        onFailure: stop|continue
        retry: 3
    inputs:
      key: default-value
```

### Templating

Steps use Go `text/template` syntax:
- `{{.ComponentName}}`
- `{{.Environment}}`
- `{{.chart}}` (from inputs)
- `{{.region}}` (from merged config)

## Output Plan Schema

```json
{
  "apiVersion": "v1",
  "kind": "Workflow",
  "metadata": {
    "name": "deployment-name",
    "description": "..."
  },
  "jobs": [
    {
      "id": "component@environment.job-name",
      "name": "job-name",
      "component": "component-name",
      "environment": "environment-name",
      "type": "component-type",
      "provider": "beeFlock",
      "capability": "helm|terraform|...",
      "steps": [
        {
          "name": "step-name",
          "run": "resolved shell command",
          "timeout": "5m",
          "retry": 3,
          "onFailure": "stop"
        }
      ],
      "dependsOn": ["other-job@env.job"],
      "timeout": "15m",
      "retries": 2,
      "env": { "fully merged config" },
      "labels": { "team": "platform", ... },
      "config": { "same as env" }
    }
  ]
}
```

## Planner Phases

### Phase 0: Load & Validate
- Load intent.yaml, jobs.yaml, schemas
- Validate against schemas
- Fail fast on schema violations

### Phase 1: Normalize
- Resolve wildcards in selectors
- Default missing fields
- Normalize dependency references

### Phase 2: Expand (Env × Component)
- For each environment, select components
- Skip disabled components
- Merge inputs per precedence
- Resolve policies (constraint validation)

### Phase 3: Job Binding
- Match component type to job definition
- Create JobInstance per component per environment
- Render templates with merged config

### Phase 4: Dependency Resolution
- Convert component dependencies → job dependencies
- Handle scope (same-environment, cross-environment)
- Resolve `environment: ""` to actual environment

### Phase 5: DAG Validation
- Topological sort (detect cycles)
- Verify all dependencies resolve

### Phase 6: Materialize
- Render final plan.json/plan.yaml
- All templates resolved
- All references concrete

## Key Features

✅ **Policy-Aware** - Group policies are non-negotiable constraints  
✅ **Schema-Driven** - Everything validated against schemas  
✅ **Deterministic** - Plan is fully determined by inputs  
✅ **Runtime-Agnostic** - Plan works with any CI executor  
✅ **Debuggable** - Debug IR dumps at each phase  
✅ **Extensible** - New component types via job registry  

## Future Enhancements

- [ ] Schema validation with JSON Schema v5
- [ ] `plan diff` - Compare two plans
- [ ] `plan --filter` - Filter by environment/components
- [ ] `plan --dry-run` - Test without output
- [ ] Template validation (catch unresolved vars)
- [ ] DAG visualization (DOT format)
- [ ] Incremental planning (changed components only)

## Development

### Testing

```bash
go test ./...
```

### Debug Mode

```bash
./liteci plan --debug -i examples/intent.yaml -j examples/jobs.yaml
```

Outputs detailed logs of each phase.

## CNCF Alignment

- **OCI-compliant** - Uses standard YAML/JSON
- **Multi-environment** - Built-in env handling
- **Declarative** - Intent-based, not imperative
- **Policy-first** - Governance at core
- **Extensible** - Schema-driven, not hardcoded

## License

MIT
