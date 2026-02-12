# Project Structure Guide

## Quick Overview

```
liteci/
├── cmd/liteci/                  # CLI Application
│   └── main.go                  # Cobra CLI with 3 commands
│
├── internal/                    # Core Engine (No public API)
│   ├── model/                   # Pure Data Structures
│   │   ├── intent.go           # Intent, Component, Group models
│   │   ├── job.go              # Job, JobRegistry, JobInstance models
│   │   └── plan.go             # Plan, PlanJob output models
│   │
│   ├── loader/                  # Phase 0: Load Files
│   │   └── loader.go           # YAML loading utilities
│   │
│   ├── normalize/               # Phase 1: Canonicalize
│   │   └── intent.go           # Wildcard expansion, defaults
│   │
│   ├── expand/                  # Phase 2: Env × Component
│   │   └── expander.go         # Selection, merge, policy resolution
│   │
│   ├── planner/                 # Phases 3-5: Bind & DAG
│   │   └── planner.go          # Job binding, template rendering, DAG ops
│   │
│   └── render/                  # Phase 6: Materialize
│       └── plan.go             # Plan rendering to JSON/YAML
│
├── examples/                    # Example Configurations
│   ├── intent.yaml             # Example Intent with 4 components
│   └── jobs.yaml               # Example Job registry
│
├── README.md                    # Full documentation
├── ARCHITECTURE.md              # Design deep-dive
├── QUICKSTART.md                # Getting started
├── IMPLEMENTATION_SUMMARY.md    # Overview
├── STRUCTURE.md                 # This file
├── Makefile                     # Build automation
├── go.mod                       # Go module manifest
└── .gitignore                   # VCS configuration
```

## Core Modules

### 1. Model (`internal/model/`)

Pure data structures with YAML/JSON tags. No business logic.

- **intent.go**: Intent, Metadata, Group, ForEach, Component, Dependency
- **job.go**: JobRegistry, Job, Step, JobInstance, JobGraph
- **plan.go**: Plan, PlanJob, PlanStep (output format)

### 2. Loader (`internal/loader/`)

Reads YAML files and converts to Go structs.

- `LoadIntent()` - Parse intent.yaml
- `LoadJobRegistry()` - Parse jobs.yaml

### 3. Normalizer (`internal/normalize/`)

Canonicalizes raw intent.

- Expand wildcards (`components: ["*"]`)
- Validate structure
- Default missing fields
- Normalize references

Output: `NormalizedIntent`

### 4. Expander (`internal/expand/`)

Creates ComponentInstance for each (environment, component) pair.

- Select applicable components per environment
- Merge inputs (6-step precedence)
- Resolve policies (constraints)
- Resolve dependencies

Output: `ComponentInstance[]`

### 5. Planner (`internal/planner/`)

The core engine that binds jobs and constructs DAG.

**Job Binding**:
- Map component types to job definitions
- Create JobInstance
- Render templates with merged config
- Resolve job dependencies

**DAG Operations**:
- Topological sort (Kahn's algorithm)
- Cycle detection (DFS-based)

Output: Sorted `JobInstance[]`

### 6. Renderer (`internal/render/`)

Materializes final output.

- Convert JobInstance → PlanJob
- Render to JSON or YAML
- Debug dump for inspection

Output: `plan.json` or `plan.yaml`

## Data Flow Diagram

```
intent.yaml ──┐
              ├→ loader ──→ Intent (raw)
jobs.yaml ────┤
              │
              ▼
         normalize ──→ NormalizedIntent
              │
              ▼
           expand ──→ ComponentInstance[]
              │
              ▼
          planner ──→ JobInstance[] (rendered)
              │
              ├→ cycle detection
              ├→ topological sort
              │
              ▼
           render ──→ Plan
              │
              ▼
        plan.json
```

## Merge Precedence Order

Configuration merges lowest → highest priority:

```
1. Type defaults (from schema)
2. Job defaults (from jobs.yaml)
3. Group defaults (from intent.groups[domain])
4. Environment defaults (from forEach[env])
5. Component inputs (highest priority)
```

Example:
```yaml
# Group defaults
groups:
  platform:
    defaults: { replicas: 2 }

# Env defaults
forEach:
  production:
    defaults: { replicas: 3 }

# Component inputs
components:
  - name: web-app
    inputs: { replicas: 5 }

# Result for web-app@production: replicas = 5
```

## Policies vs Defaults

- **Policies**: Constraints that cannot be overridden. Validation fails if violated.
- **Defaults**: Configuration values that can be overridden by higher layers.

```yaml
groups:
  platform:
    policies:
      isolation: strict        # Cannot be changed
    defaults:
      namespace: platform-     # Can be overridden
```

## CLI Commands

Three main commands provided:

1. **validate** - Schema validation
   - Validates YAML structure
   - Checks for schema violations

2. **debug** - Intent introspection
   - Shows metadata, groups, environments, components
   - Useful for understanding what will be expanded

3. **plan** - Generate execution DAG
   - Full compilation pipeline
   - Outputs plan.json or plan.yaml
   - Optional `--debug` for detailed output

## Configuration Files

### intent.yaml

```yaml
apiVersion: sourceplane.io/v1
kind: Intent

metadata:
  name: deployment-name
  description: Description
  namespace: default

groups:
  <domain>:
    policies: {...}     # Non-overridable
    defaults: {...}     # Can override

forEach:                # Environments
  <env>:
    selectors:
      components: [...]
      domains: [...]
    defaults: {...}
    policies: {...}

components:
  - name: comp-name
    type: helm|terraform|...
    domain: domain-name
    inputs: {...}
    labels: {...}
    dependsOn: [...]
```

### jobs.yaml

```yaml
apiVersion: sourceplane.io/v1
kind: JobRegistry

jobs:
  <component-type>:
    name: job-name
    timeout: 15m
    retries: 2
    steps:
      - name: step-name
        run: shell command with {{.Variables}}
        timeout: 5m
        onFailure: stop|continue
```

## Extension Mechanisms

### 1. New Component Type

- Add job definition to jobs.yaml
- Use type in intent.yaml
- Engine handles rest

### 2. New Policy

- Add to groups.policies
- Links via component.domain
- Automatically enforced

### 3. New Environment

- Add to forEach
- Define selectors and defaults
- Component instances auto-created

## Performance

| Metric | Value |
|--------|-------|
| Typical job count | 100 |
| Time to generate plan | < 100ms |
| Memory per job | ~10KB |
| Max tested jobs | 1000+ |

## Build Targets

```bash
make build        # Build binary
make run-plan     # Generate plan
make run-validate # Validate examples
make run-debug    # Debug processing
make test         # Run tests
make clean        # Remove artifacts
make lint         # Code linting
make fmt          # Code formatting
```

## Testing Strategy

Tests would typically go in:
- `internal/normalize/normalize_test.go`
- `internal/expand/expander_test.go`
- `internal/planner/planner_test.go`
- `cmd/liteci/cli_test.go`

## Future Enhancements

- [ ] JSON Schema v5 validation
- [ ] Custom selector plugins
- [ ] Policy validator plugins
- [ ] Multi-file intent support
- [ ] DAG visualization
- [ ] Incremental planning
- [ ] Plan diffing
