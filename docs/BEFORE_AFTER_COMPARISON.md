# Before & After - Jobs Structure Redesign

## Before: Single Job Per Model

### Job Definition Structure
```yaml
# component-models/helm/job.yaml (OLD)
name: deploy
description: Deploy using Helm
timeout: 15m
retries: 2
steps:
  - name: add-repository
    run: helm repo add mycompany ...
  - name: update-repo
    run: helm repo update
  - name: deploy
    run: helm upgrade --install ...
  - name: verify
    run: kubectl get deployment ...
inputs:
  pullPolicy: IfNotPresent
```

### Go Model Structure
```go
// OLD: Job as a map
type JobRegistry struct {
    APIVersion string           
    Kind       string           
    Jobs       map[string]*Job  // Single job per type
}

type Job struct {
    Name        string
    Description string
    Timeout     string
    Retries     int
    Steps       []Step
    Inputs      map[string]interface{}
}
```

### Limitations
❌ Only one job per model
❌ No way to express alternative workflows
❌ Rollback/recovery required separate model
❌ No job metadata or categorization
❌ No platform or version constraints
❌ No declarative job bindings

---

## After: Multiple Jobs Per Model with K8s-Style Bindings

### Job Definition Structure
```yaml
# component-models/helm/job.yaml (NEW)
apiVersion: sourceplane.io/v1
kind: JobRegistry
metadata:
  name: helm-jobs
  description: Helm deployment jobs

jobs:
  - name: deploy
    description: Deploy using Helm
    timeout: 15m
    retries: 2
    labels:
      scope: deployment
      tier: application
    steps:
      - name: add-repository
        run: helm repo add mycompany ...
      - name: update-repo
        run: helm repo update
      - name: deploy
        run: helm upgrade --install ...
      - name: verify
        run: kubectl get deployment ...
    inputs:
      pullPolicy: IfNotPresent

  - name: rollback
    description: Rollback Helm release
    timeout: 10m
    retries: 1
    labels:
      scope: recovery
      tier: application
    steps:
      - name: get-revision
        run: helm history {{.Component}} ...
      - name: rollback
        run: helm rollback {{.Component}} ...
    inputs:
      pullPolicy: IfNotPresent

  - name: diff
    description: Show differences
    timeout: 5m
    retries: 0
    labels:
      scope: analysis
    steps:
      - name: diff
        run: helm diff release ...
```

### Job Bindings (K8s-Style)
```yaml
# examples/job-bindings.yaml (NEW)
apiVersion: sourceplane.io/v1
kind: JobBinding
metadata:
  name: helm-jobs
  namespace: default
spec:
  model: helm
  defaultJob: deploy
  jobs:
    - name: deploy
      required: true
    - name: rollback
      required: false
    - name: diff
      required: false
  constraints:
    platforms:
      - kubernetes
    minVersion: "3.0"
```

### Go Model Structure
```go
// NEW: Jobs as array with fast lookup
type JobRegistry struct {
    APIVersion string      
    Kind       string      
    Metadata   Metadata    
    Jobs       []JobSpec   // Multiple jobs, array format
}

type JobSpec struct {
    Name        string
    Description string
    Timeout     string
    Retries     int
    Steps       []Step
    Inputs      map[string]interface{}
    Labels      map[string]string  // ✅ NEW: Job categorization
}

type JobBinding struct {
    APIVersion string
    Kind       string
    Metadata   Metadata
    Spec       JobBindingSpec
}

type JobBindingSpec struct {
    Model       string          // Model name
    Jobs        []JobRef        // Available jobs
    DefaultJob  string          // Default job selection
    Constraints JobConstraints  // Platform/version constraints
}

// ComponentType structure
type ComponentType struct {
    Name     string
    Jobs     []JobSpec                  // ✅ All jobs
    JobMap   map[string]*JobSpec        // ✅ Fast lookup
    Schema   *jsonschema.Schema
    Bindings *JobBinding                 // ✅ Declarative binding
}
```

### Improvements
✅ Multiple jobs per model
✅ Clear default job selection
✅ Job labels for categorization
✅ K8s-style declarative bindings
✅ Platform and version constraints
✅ Extensible metadata (labels, constraints)
✅ Fast job lookup (JobMap)
✅ Backward compatible execution

---

## Comparison: Capabilities

| Feature | Before | After |
|---------|--------|-------|
| Jobs per model | 1 | Unlimited |
| Job metadata | None | Labels, constraints |
| Job categorization | None | By scope/tier/type |
| Bindings style | Implicit | K8s declarative |
| Default job | Always used | Explicitly declared |
| Platform support | None | Configurable |
| Version constraints | None | Supported |
| Fast job lookup | Not needed | Built-in (JobMap) |
| Extensibility | Limited | Full with metadata |

---

## Model Availability Summary

### Helm Model
| Aspect | Before | After |
|--------|--------|-------|
| Jobs | 1 (deploy) | 3 (deploy, rollback, diff) |
| Scope | Deployment only | Deployment, recovery, analysis |
| Default | N/A | deploy |

### Terraform Model
| Aspect | Before | After |
|--------|--------|-------|
| Jobs | 1 (plan) | 3 (plan, apply, destroy) |
| Scope | Analysis only | Analysis, deployment, cleanup |
| Default | N/A | plan |

### Charts Model
| Aspect | Before | After |
|--------|--------|-------|
| Jobs | 1 (package) | 3 (package, publish, test) |
| Scope | Packaging | Packaging, publishing, testing |
| Default | N/A | package |

### HelmCommon Model
| Aspect | Before | After |
|--------|--------|-------|
| Jobs | 1 (deploy) | 3 (deploy, health-check, rollback) |
| Scope | Deployment | Deployment, monitoring, recovery |
| Default | N/A | deploy |

---

## Plan Generation: Before vs After

### Before
```
Plan Output:
- 10 jobs generated
- All use the single defined job (deploy/plan/package)
- No job variant options
```

### After
```
Plan Output:
- 10 jobs generated
- Using default jobs (deploy for helm/helmCommon, plan for terraform, package for charts)
- Additional jobs available per model:
  - Helm: rollback, diff
  - Terraform: apply, destroy
  - Charts: publish, test
  - HelmCommon: health-check, rollback
```

---

## File Changes Summary

| File | Status | Changes |
|------|--------|---------|
| `internal/model/job.go` | ✅ Updated | JobRegistry array, JobBinding, labels, constraints |
| `internal/loader/loader.go` | ✅ Updated | Build JobMap, support array structure |
| `internal/planner/planner.go` | ✅ Updated | Work with ComponentTypeInfo |
| `cmd/liteci/main.go` | ✅ Updated | Build component type info map |
| `cmd/liteci/models.go` | ✅ Updated | Handle multiple jobs |
| `component-models/helm/job.yaml` | ✅ Updated | 3 jobs, JobRegistry format |
| `component-models/terraform/job.yaml` | ✅ Updated | 3 jobs, JobRegistry format |
| `component-models/charts/job.yaml` | ✅ Updated | 3 jobs, JobRegistry format |
| `component-models/helmCommon/job.yaml` | ✅ Updated | 3 jobs, JobRegistry format |
| `examples/jobs.yaml` | ✅ Updated | New format demonstration |
| `examples/job-bindings.yaml` | ✅ New | K8s-style job bindings |
| `JOB_BINDINGS.md` | ✅ New | Comprehensive documentation |
| `JOBS_RESTRUCTURING.md` | ✅ New | Summary of changes |

---

## Backward Compatibility Verification

✅ **Command Line**
- `liteci models` - Works identically
- `liteci model <name>` - Shows new job indicator
- `liteci plan` - Generates same output structure

✅ **Execution**
- Plans contain identical job definitions
- Step templates work unchanged
- Default job selection automatic

✅ **Data Format**
- plan.json structure unchanged
- Component definitions compatible
- Intent YAML not affected

✅ **Performance**
- Fast job lookup via JobMap
- No performance regression
- Efficient schema validation

---

## Migration Path for Users

1. **Existing Users**: No action needed - system is backward compatible
2. **New Projects**: Can leverage multiple jobs per model
3. **Future Features**: 
   - Explicit job selection in intent
   - Conditional job execution
   - Job dependencies
   - Dynamic job bindings

