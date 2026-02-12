# JobRegistry Binding Update - Complete ✅

## What Changed

The plan and model descriptions now **clearly reflect the JobRegistry structure** and show the **binding between component models and their JobRegistry definitions**.

---

## Key Improvements

### 1. **Model Display Shows JobRegistry Binding**

**Before:**
```
Model: helm
Job Information:
  Name:        deploy
  Description: Deploy using Helm (and 2 more jobs available)
```

**After:**
```
Component Model: helm

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
```

✅ **Crystal Clear**: Shows the JobRegistry name, description, total jobs, and lists each with scope and metadata

---

### 2. **Plan Includes JobRegistry Bindings**

**New Plan Structure:**
```json
{
  "apiVersion": "sourceplane.io/v1",
  "kind": "Workflow",
  "metadata": {...},
  "spec": {
    "jobBindings": {
      "charts": "charts-jobs",
      "helm": "helm-jobs",
      "helmCommon": "helmcommon-jobs",
      "terraform": "terraform-jobs"
    }
  },
  "jobs": [...]
}
```

✅ **Transparent Mapping**: Shows exactly which JobRegistry each model uses

---

### 3. **Each Plan Job References Its JobRegistry**

**Before:**
```json
{
  "id": "web-app@production.deploy",
  "name": "deploy",
  "component": "web-app",
  "environment": "production",
  "type": "helm",
  "steps": [...],
  ...
}
```

**After:**
```json
{
  "id": "web-app@production.deploy",
  "name": "deploy",
  "component": "web-app",
  "environment": "production",
  "type": "helm",
  "jobRegistry": "helm-jobs",      ← NEW: Registry name
  "job": "deploy",                  ← NEW: Job name
  "steps": [...],
  ...
}
```

✅ **Explicit Reference**: Every job shows its JobRegistry and the specific job used

---

## Complete JobRegistry Binding Map

| Component Model | JobRegistry | Jobs |
|---|---|---|
| **helm** | helm-jobs | ★deploy, rollback, diff |
| **terraform** | terraform-jobs | ★plan, apply, destroy |
| **charts** | charts-jobs | ★package, publish, test |
| **helmCommon** | helmcommon-jobs | ★deploy, health-check, rollback |

(★ indicates default job)

---

## How to Use

### View Model with JobRegistry Binding
```bash
$ ./liteci model helm

Component Model: helm

JobRegistry Binding:
  Registry Name: helm-jobs
  Registry Desc: Helm deployment jobs
  Default Job:   deploy
  Total Jobs:    3

Available Jobs (from JobRegistry):
★ 1. deploy [deployment]
  2. rollback [recovery]
  3. diff [analysis]
```

### Check Plan's JobRegistry Bindings
```bash
$ cat plan.json | jq '.spec.jobBindings'

{
  "charts": "charts-jobs",
  "helm": "helm-jobs",
  "helmCommon": "helmcommon-jobs",
  "terraform": "terraform-jobs"
}
```

### See Which Registry a Job Uses
```bash
$ cat plan.json | jq '.jobs[] | {component, type, jobRegistry, job}'

{
  "component": "common-services",
  "type": "helmCommon",
  "jobRegistry": "helmcommon-jobs",
  "job": "deploy"
}
{
  "component": "web-app",
  "type": "helm",
  "jobRegistry": "helm-jobs",
  "job": "deploy"
}
{
  "component": "web-app-infra",
  "type": "terraform",
  "jobRegistry": "terraform-jobs",
  "job": "plan"
}
```

---

## Implementation Details

### ModelInfo Structure
```go
type ModelInfo struct {
    // Existing
    Name            string
    Description     string
    
    // NEW: JobRegistry Binding
    JobRegistryName string              // e.g., "helm-jobs"
    JobRegistryDesc string              // e.g., "Helm deployment jobs"
    AvailableJobs   []JobBindingInfo    // All jobs in the registry
    DefaultJobName  string              // Default job (e.g., "deploy")
    
    // Current Job Details
    JobName         string              // Default job name
    JobDescription  string              // Default job description
    Steps           []StepInfo
}

type JobBindingInfo struct {
    Name        string                  // Job name
    Description string                  // Job description
    Scope       string                  // Scope label (deployment, recovery, etc)
    Steps       int                     // Number of steps
    Timeout     string                  // Job timeout
}
```

### Plan Structure
```go
type Plan struct {
    APIVersion string
    Kind       string
    Metadata   Metadata
    Spec       PlanSpec           // NEW: Contains JobBindings
    Jobs       []PlanJob
}

type PlanSpec struct {
    JobBindings map[string]string  // model → JobRegistry name
}

type PlanJob struct {
    // Existing
    ID          string
    Name        string
    Component   string
    Type        string              // Component model type
    
    // NEW: JobRegistry References
    JobRegistry string              // JobRegistry name being used
    Job         string              // Job name from that registry
    
    Steps       []PlanStep
    // ... rest of fields
}
```

---

## Files Modified

### Core Implementation
- ✅ `internal/model/plan.go` - Added PlanSpec with JobBindings
- ✅ `internal/render/plan.go` - Updated RenderPlan to include bindings
- ✅ `cmd/liteci/main.go` - Build JobBindings map during plan generation

### Display/UX
- ✅ `cmd/liteci/models.go` - Enhanced ModelInfo with JobRegistry details
- ✅ Display functions show JobRegistry name, total jobs, available jobs list

---

## Testing Summary

✅ **Model Display Test**
```
$ ./liteci model helm
Shows:
- JobRegistry Name: helm-jobs
- Registry Description: Helm deployment jobs
- Default Job: deploy (marked with ★)
- Total Jobs: 3
- All available jobs with scope and metadata
```

✅ **Plan Generation Test**
```
$ ./liteci plan --intent examples/intent.yaml -o plan.json
Generates plan with:
- spec.jobBindings mapping all 4 models to their registries
- Each job includes jobRegistry and job fields
- All 10 jobs reference their respective registries
```

✅ **Job Reference Test**
```
$ cat plan.json | jq '.jobs[0] | {type, jobRegistry, job}'
{
  "type": "helm",
  "jobRegistry": "helm-jobs",
  "job": "deploy"
}
```

---

## Key Points

### What's Clear Now

1. **Component Model ↔ JobRegistry Binding**
   - Each model is explicitly bound to a JobRegistry
   - `helm` model uses `helm-jobs` registry
   - `terraform` model uses `terraform-jobs` registry
   - etc.

2. **Available Jobs Per Registry**
   - Model display lists all jobs in the bound registry
   - Shows job metadata (scope, description, steps, timeout)
   - Default job is marked with ★

3. **Plan Traceability**
   - Each plan job shows its JobRegistry and job name
   - Plan spec contains complete binding map
   - Easy to audit which registry/job is being used

4. **Future Extensibility**
   - Ready for explicit job selection
   - Ready for conditional job execution
   - Ready for dynamic bindings

---

## Summary

The JobRegistry binding is now **fully visible and transparent**:

✅ Component models are explicitly bound to JobRegistries
✅ Model displays show the JobRegistry name and all available jobs
✅ Plans document which JobRegistry and job each execution uses
✅ Complete binding map in plan spec for audit and traceability
✅ Self-documenting structure enables future enhancements

**Result**: Users and operators can clearly see:
- How component models map to job registries
- What jobs are available in each registry
- Which jobs are being used in any given plan
- How the system binds models to their job definitions
