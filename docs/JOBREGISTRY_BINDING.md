# JobRegistry and Component Model Binding - Enhanced Clarity

## Overview

The lite-ci framework now provides **explicit, transparent binding between component models and their JobRegistry definitions**. Every plan shows:

1. **The JobRegistry it uses** (e.g., `helm-jobs`, `terraform-jobs`)
2. **All available jobs** in that registry with scopes and metadata
3. **The default job** being used in the plan
4. **Clear mapping** between component models and their job registries

---

## Component Model ↔ JobRegistry Binding

### Directory Structure Shows Binding

```
component-models/
├── helm/                      ← Component Model
│   ├── job.yaml              ← Defines: JobRegistry "helm-jobs" with 3 jobs
│   └── schema.yaml           ← Component validation schema
├── terraform/                 ← Component Model
│   ├── job.yaml              ← Defines: JobRegistry "terraform-jobs" with 3 jobs
│   └── schema.yaml
├── charts/                    ← Component Model
│   ├── job.yaml              ← Defines: JobRegistry "charts-jobs" with 3 jobs
│   └── schema.yaml
└── helmCommon/               ← Component Model
    ├── job.yaml              ← Defines: JobRegistry "helmcommon-jobs" with 3 jobs
    └── schema.yaml
```

### The Binding Relationship

```
Component Model (helm)
        ↓
    job.yaml
        ↓
apiVersion: sourceplane.io/v1
kind: JobRegistry
metadata:
  name: helm-jobs              ← Registry Name
  description: Helm deployment jobs
jobs:
  - name: deploy              ← Job #1 (default)
  - name: rollback            ← Job #2
  - name: diff                ← Job #3
```

---

## Model Display Shows JobRegistry Binding

### Before
```
Model: helm
Description: Model: helm

Job Information:
  Name:        deploy
  Description: Deploy using Helm (and 2 more jobs available)
```

### After
```
Component Model: helm
Description: Model: helm

JobRegistry Binding:
  Registry Name: helm-jobs
  Registry Desc: Helm deployment jobs
  Default Job:   deploy
  Total Jobs:    3

Available Jobs (from JobRegistry):
★ 1. deploy [deployment]
     Description: Deploy using Helm
     Steps: 4 | Timeout: 15m

  2. rollback [recovery]
     Description: Rollback Helm release to previous version
     Steps: 3 | Timeout: 10m

  3. diff [analysis]
     Description: Show differences between deployed and intended Helm release
     Steps: 2 | Timeout: 5m

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Using Default Job: deploy
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

**Clear Separation**: Shows which JobRegistry is bound, lists all jobs available, marks default with ★, then shows details of the default job being used.

---

## Plan Shows JobRegistry Bindings

### Plan Structure (plan.json)

```json
{
  "apiVersion": "sourceplane.io/v1",
  "kind": "Workflow",
  "metadata": {
    "name": "microservices-deployment"
  },
  "spec": {
    "jobBindings": {
      "charts": "charts-jobs",
      "helm": "helm-jobs",
      "helmCommon": "helmcommon-jobs",
      "terraform": "terraform-jobs"
    }
  },
  "jobs": [
    {
      "id": "common-services@staging.deploy",
      "name": "deploy",
      "component": "common-services",
      "environment": "staging",
      "type": "helmCommon",
      "jobRegistry": "helmcommon-jobs",        ← Which registry
      "job": "deploy",                         ← Which job from registry
      "steps": [...],
      "dependsOn": [],
      "timeout": "10m",
      "retries": 2,
      ...
    },
    {
      "id": "web-app@development.deploy",
      "name": "deploy",
      "component": "web-app",
      "environment": "development",
      "type": "helm",
      "jobRegistry": "helm-jobs",              ← helm uses helm-jobs
      "job": "deploy",                         ← using deploy job
      "steps": [...],
      ...
    },
    {
      "id": "web-app-infra@production.plan",
      "name": "plan",
      "component": "web-app-infra",
      "environment": "production",
      "type": "terraform",
      "jobRegistry": "terraform-jobs",         ← terraform uses terraform-jobs
      "job": "plan",                           ← using plan job
      "steps": [...],
      ...
    }
  ]
}
```

### Plan Spec: JobBindings Section

```json
"spec": {
  "jobBindings": {
    "charts": "charts-jobs",
    "helm": "helm-jobs",
    "helmCommon": "helmcommon-jobs",
    "terraform": "terraform-jobs"
  }
}
```

**Mapping**: Shows exactly which JobRegistry each component model uses.

### Plan Job: JobRegistry and Job Fields

Each job in the plan now includes:
- `"type"`: Component model type (helm, terraform, charts, helmCommon)
- `"jobRegistry"`: Name of the JobRegistry being used (helm-jobs, terraform-jobs, etc)
- `"job"`: The specific job name from that registry (deploy, plan, rollback, etc)

---

## All Job Registry Bindings

### JobRegistry Names by Component Model

| Component Model | JobRegistry Name | Jobs |
|---|---|---|
| `helm` | `helm-jobs` | deploy, rollback, diff |
| `terraform` | `terraform-jobs` | plan, apply, destroy |
| `charts` | `charts-jobs` | package, publish, test |
| `helmCommon` | `helmcommon-jobs` | deploy, health-check, rollback |

---

## Command Examples

### View Model with JobRegistry Binding
```bash
$ ./liteci model terraform

Component Model: terraform
Description: Model: terraform

JobRegistry Binding:
  Registry Name: terraform-jobs
  Registry Desc: Terraform infrastructure jobs
  Default Job:   plan
  Total Jobs:    3

Available Jobs (from JobRegistry):
★ 1. plan [analysis]
     Description: Plan Terraform configuration
     Steps: 4 | Timeout: 20m

  2. apply [deployment]
     Description: Apply Terraform configuration
     Steps: 5 | Timeout: 30m

  3. destroy [cleanup]
     Description: Destroy Terraform managed infrastructure
     Steps: 3 | Timeout: 25m
```

### View Plan JobRegistry Bindings
```bash
$ cat plan.json | jq '.spec.jobBindings'

{
  "charts": "charts-jobs",
  "helm": "helm-jobs",
  "helmCommon": "helmcommon-jobs",
  "terraform": "terraform-jobs"
}
```

### View Job with Registry Reference
```bash
$ cat plan.json | jq '.jobs[0]'

{
  "id": "common-services@staging.deploy",
  "name": "deploy",
  "component": "common-services",
  "environment": "staging",
  "type": "helmCommon",
  "jobRegistry": "helmcommon-jobs",     ← Clear registry binding
  "job": "deploy",                      ← Clear job selection
  "steps": [
    {
      "name": "add-repository",
      "run": "helm repo add ...",
      "retry": 3
    },
    ...
  ]
}
```

---

## Data Model Enhancements

### PlanJob - New Fields

```go
type PlanJob struct {
    // Existing fields
    ID          string                 
    Name        string                 
    Component   string                 
    Environment string                 
    Type        string                 // Component model type
    
    // NEW: JobRegistry binding clarity
    JobRegistry string                 // Name of the JobRegistry (e.g., "helm-jobs")
    Job         string                 // Specific job from that registry
    
    Steps       []PlanStep             
    DependsOn   []string               
    Timeout     string                 
    Retries     int                    
    Env         map[string]interface{} 
    Labels      map[string]string      
    Config      map[string]interface{} 
}
```

### Plan.Spec - New Bindings

```go
type PlanSpec struct {
    JobBindings map[string]string  // model type → JobRegistry name
}

type Plan struct {
    APIVersion string
    Kind       string
    Metadata   Metadata
    Spec       PlanSpec           // NEW: Contains JobBindings
    Jobs       []PlanJob
}
```

---

## How It Works: End-to-End

### 1. Component Model Definition
```yaml
# component-models/helm/job.yaml
apiVersion: sourceplane.io/v1
kind: JobRegistry
metadata:
  name: helm-jobs
  description: Helm deployment jobs
jobs:
  - name: deploy
    description: Deploy using Helm
    ...
```

### 2. Plan Generation
- Loader reads `helm/job.yaml` and extracts `helm-jobs` as JobRegistry name
- All 3 jobs loaded into ComponentType.Jobs array
- First job (deploy) selected as default

### 3. Model Display
```bash
$ ./liteci model helm

JobRegistry Binding:
  Registry Name: helm-jobs
  Total Jobs: 3

Available Jobs:
★ 1. deploy
  2. rollback
  3. diff
```

### 4. Plan Execution
```json
{
  "spec": {
    "jobBindings": {
      "helm": "helm-jobs"
    }
  },
  "jobs": [
    {
      "type": "helm",
      "jobRegistry": "helm-jobs",
      "job": "deploy"
    }
  ]
}
```

---

## Benefits of Explicit JobRegistry Binding

✅ **Clear Mapping**: Easy to see which JobRegistry each model uses
✅ **Audit Trail**: Plan shows exactly which registry and job was used
✅ **Discoverability**: `liteci model <name>` shows all available jobs
✅ **Future-Ready**: Easy to add job selection in intent
✅ **Documentation**: Self-documenting plan structure
✅ **Extensibility**: Can add bindings, constraints, conditionals

---

## Summary

The JobRegistry binding is now **explicit and transparent**:

1. **File Level**: Each component model's `job.yaml` defines its JobRegistry
2. **Display Level**: `liteci model` shows the JobRegistry binding and all jobs
3. **Plan Level**: Each job references its JobRegistry name and specific job used
4. **Spec Level**: Plan spec contains the complete model → JobRegistry mapping

This makes it immediately clear:
- Which JobRegistry each component model is bound to
- What jobs are available in each registry
- Which job and registry each plan job uses
