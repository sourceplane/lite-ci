# liteci - Quick Start Guide

## What is liteci?

**liteci** is a schema-driven planner engine that transforms declarative intent into deterministic execution DAGs. Think of it as a "CI compiler":

```
Intent.yaml (what?)  +  Jobs.yaml (how?)  →  Plan.json (execution DAG)
```

## Key Concepts

### 1. **Intent** - Policy-Aware Declaration
What you want to deploy, with policies and constraints:

```yaml
# Define policy domains
groups:
  platform:
    policies:           # Non-overridable constraints
      isolation: strict
    defaults:           # Overridable defaults
      namespace: platform-

# Define environments
forEach:
  production:
    selectors:
      components: ["web-app", "common-services"]
    defaults:
      replicas: 3
    policies:
      requireApproval: true

# Declare components
components:
  - name: web-app
    type: helm          # Links to job definition
    domain: platform    # Links to policy group
    inputs:
      chart: oci://...
```

### 2. **Jobs** - Execution Templates
How each component type runs:

```yaml
jobs:
  helm:
    name: deploy
    steps:
      - name: deploy
        run: helm upgrade --install {{.Component}} {{.chart}}
```

### 3. **Plan** - Immutable DAG
Fully expanded, deterministic execution plan:

```json
{
  "jobs": [
    {
      "id": "web-app@production.deploy",
      "steps": [
        {
          "run": "helm upgrade --install web-app oci://... --replicas 3"
        }
      ],
      "dependsOn": ["common-services@production.deploy"]
    }
  ]
}
```

## Installation

```bash
cd /Users/irinelinson/sourceplane/devops-provider-bee-hive-style-gha-eks
go build -o liteci ./cmd/liteci
```

## Basic Usage

### 1. Validate Configuration

```bash
./liteci validate -i intent.yaml -j jobs.yaml
```

Output:
```
✓ Intent is valid
✓ Job registry is valid (4 jobs)
✓ All validation passed
```

### 2. Inspect Intent

```bash
./liteci debug -i intent.yaml -j jobs.yaml
```

Shows:
- Metadata
- Groups (policy domains)
- Environments and selectors
- Components and their types

### 3. Generate Plan

```bash
./liteci plan -i intent.yaml -j jobs.yaml -o plan.json
```

Or with debugging:

```bash
./liteci plan -i intent.yaml -j jobs.yaml --debug
```

## How It Works

### Phase 1: Normalize
- Load intent.yaml
- Expand wildcards (`components: ["*"]`)
- Validate schema
- Canonicalize structure

### Phase 2: Expand (Env × Component)
For each environment:
- Select applicable components
- Merge configuration (precedence: group defaults → env defaults → component inputs)
- Apply policies (constraints)
- Resolve dependencies

### Phase 3: Bind Jobs
- Map component types to job definitions
- Create JobInstance for each component instance
- Render step templates with merged config
- Resolve job-level dependencies

### Phase 4: Validate DAG
- Detect cycles (fail if found)
- Topologically sort jobs

### Phase 5: Materialize
- Output immutable plan.json
- Ready for any CI runner to execute

## Configuration Merge Order

Configuration is merged in this order (lowest to highest priority):

1. **Type Defaults** - From schema
2. **Job Defaults** - From jobs.yaml
3. **Group Defaults** - From intent groups (domain)
4. **Environment Defaults** - From intent forEach
5. **Component Inputs** - From intent components

Example:

```yaml
groups:
  platform:
    defaults:
      replicas: 2

forEach:
  production:
    defaults:
      replicas: 3

components:
  - name: web-app
    inputs:
      replicas: 5
```

Result for `web-app@production`: `replicas: 5` (component inputs win)

## Policies

Policies are **constraints that cannot be overridden**:

```yaml
groups:
  platform:
    policies:
      isolation: strict  # Cannot be overridden!
```

If a component violates a policy, planning fails.

## Templating

Step commands use Go templates:

```yaml
steps:
  - run: helm upgrade --install {{.Component}} {{.chart}} --replicas {{.replicas}}
```

Available variables:
- `{{.Component}}` - Component name
- `{{.Environment}}` - Environment name
- `{{.Type}}` - Component type
- `{{.any_input_key}}` - Any merged configuration key

## Examples

### Example 1: Simple Web App

intent.yaml:
```yaml
components:
  - name: web-app
    type: helm
    inputs:
      chart: oci://mycompany.com/web-app

forEach:
  production:
    selectors:
      components: [web-app]
    defaults:
      replicas: 3
```

jobs.yaml:
```yaml
jobs:
  helm:
    name: deploy
    steps:
      - run: helm upgrade --install {{.Component}} {{.chart}} --replicas {{.replicas}}
```

Plan output:
```json
{
  "jobs": [{
    "id": "web-app@production.deploy",
    "steps": [{
      "run": "helm upgrade --install web-app oci://mycompany.com/web-app --replicas 3"
    }]
  }]
}
```

### Example 2: Multi-Environment with Dependencies

intent.yaml:
```yaml
components:
  - name: database
    type: terraform
    
  - name: web-app
    type: helm
    dependsOn:
      - component: database
        environment: ""    # Same as current env
        condition: success

forEach:
  production:
    selectors:
      components: [database, web-app]
    defaults:
      replicas: 3
```

Plan output creates jobs:
```
database@production.plan
    ↓
web-app@production.deploy
```

Execution waits for database before deploying web-app.

## File Structure

```
intent.yaml          # What to deploy
jobs.yaml            # How to deploy
plan.json            # Generated execution DAG
```

## Common Commands

```bash
# Validate
./liteci validate -i intent.yaml -j jobs.yaml

# Debug
./liteci debug -i intent.yaml -j jobs.yaml

# Generate plan
./liteci plan -i intent.yaml -j jobs.yaml -o plan.json

# With debug output
./liteci plan -i intent.yaml -j jobs.yaml --debug

# Filter by environment
./liteci plan -i intent.yaml -j jobs.yaml --env=production

# Output format
./liteci plan -i intent.yaml -j jobs.yaml -f yaml -o plan.yaml
```

## Troubleshooting

### "Failed to load intent"
- Check intent.yaml path is correct
- Verify YAML syntax (use `yamllint`)

### "No job definition for type: X"
- Add definition to jobs.yaml
- Check component type matches job key

### "Dependency not found"
- Check dependent component exists
- Verify component names are exact

### "Cycle detected"
- Check for circular dependencies
- Use `--debug` to see dependency graph

### "Policy constraint violation"
- Component doesn't belong to required group
- Or violates policy in its domain

## Next Steps

1. **Create intent.yaml** - Define your deployment intent
2. **Create jobs.yaml** - Define execution steps per component type
3. **Generate plan** - `./liteci plan -i intent.yaml -j jobs.yaml`
4. **Review plan.json** - Verify generated execution DAG
5. **Execute** - Hand to your CI runner (GitHub Actions, Argo, etc.)

## Design Principles

✅ **Intent is execution-agnostic** - Only declares WHAT, not HOW  
✅ **Jobs are environment-agnostic** - Templates for any env  
✅ **Plans are deterministic** - Same input = same output  
✅ **Policies are immutable** - Cannot be overridden  
✅ **Everything is schema-driven** - Validation at load time  
✅ **Runtime-agnostic** - Executor doesn't matter  

## Further Reading

- [ARCHITECTURE.md](ARCHITECTURE.md) - Deep dive into design
- [README.md](README.md) - Full feature documentation
- Examples: [examples/](examples/)

## Support

For issues or questions:
1. Check debug output: `--debug` flag
2. Review ARCHITECTURE.md for design details
3. Inspect generated plan.json structure
