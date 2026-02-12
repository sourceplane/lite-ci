# Job Structure and Job Bindings - New K8s-Style Declarative Format

## Overview

The lite-ci framework has been enhanced to support a more flexible, k8s-style declarative approach to defining and binding jobs. Each model now supports multiple jobs with multiple steps, and job bindings follow Kubernetes conventions.

## Key Changes

### 1. Multiple Jobs Per Model

Each model's `job.yaml` file now uses a `JobRegistry` format that supports multiple jobs:

```yaml
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
        run: helm repo add mycompany oci://mycompany.azurecr.io/helm/charts
        retry: 3
      - name: update-repo
        run: helm repo update
      - name: deploy
        run: helm upgrade --install {{.Component}} {{.chart}} ...
        timeout: 15m
        onFailure: stop
      - name: verify
        run: kubectl get deployment ...
        onFailure: continue
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
      - name: verify-rollback
        run: kubectl rollout status ...
    inputs:
      pullPolicy: IfNotPresent
```

### 2. K8s-Style Job Bindings

Job bindings now follow Kubernetes conventions with `apiVersion`, `kind`, `metadata`, and `spec`:

```yaml
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

**Key Fields:**
- **model**: Name of the model (helm, terraform, charts, helmCommon)
- **defaultJob**: The default job to use when not specified
- **jobs**: List of available jobs with requirement flags
- **constraints**: Optional platform and version constraints

### 3. Enhanced Job Features

#### Multiple Steps
Each job can now contain multiple steps with independent timeout, retry, and failure handling:

```yaml
steps:
  - name: init
    run: terraform init -backend=true
    onFailure: stop
  - name: fmt-check
    run: terraform fmt -check
    onFailure: continue
  - name: validate
    run: terraform validate
    timeout: 5m
    onFailure: stop
  - name: plan
    run: terraform plan -workspace={{.workspace}} -out=tfplan
    timeout: 15m
    onFailure: stop
```

#### Job Labels
Jobs can be labeled for categorization and filtering:

```yaml
labels:
  scope: deployment      # deployment, recovery, analysis, testing, etc
  tier: application      # application, infrastructure, artifacts, etc
  type: common-services
```

#### Job Timeout and Retries
Job-level configuration with step-level overrides:

```yaml
- name: deploy
  timeout: 15m       # Job timeout
  retries: 2         # Job retry count
  steps:
    - name: step1
      timeout: 5m    # Step-specific timeout
      retry: 3       # Step-specific retry
```

### 4. Model Structure

Directory structure remains the same but job.yaml now contains multiple jobs:

```
component-models/
├── helm/
│   ├── job.yaml          # Multiple jobs (deploy, rollback, diff)
│   └── schema.yaml       # Component type schema
├── terraform/
│   ├── job.yaml          # Multiple jobs (plan, apply, destroy)
│   └── schema.yaml
├── charts/
│   ├── job.yaml          # Multiple jobs (package, publish, test)
│   └── schema.yaml
└── helmCommon/
    ├── job.yaml          # Multiple jobs (deploy, health-check, rollback)
    └── schema.yaml
```

## Go Model Updates

### Updated Structures

```go
// JobSpec defines a complete job specification with multiple steps
type JobSpec struct {
    Name        string
    Description string
    Timeout     string
    Retries     int
    Steps       []Step
    Inputs      map[string]interface{}
    Labels      map[string]string  // New: job labels
}

// JobBinding is a k8s-style declarative binding
type JobBinding struct {
    APIVersion string
    Kind       string
    Metadata   Metadata
    Spec       JobBindingSpec
}

// JobBindingSpec specifies available jobs for a model
type JobBindingSpec struct {
    Model       string           // Model name
    Jobs        []JobRef         // Available jobs
    DefaultJob  string           // Default job
    Constraints JobConstraints   // Platform/version constraints
}
```

### ComponentType Registry

```go
// ComponentType holds all jobs and schemas for a model
type ComponentType struct {
    Name     string
    Jobs     []JobSpec                  // All jobs for this model
    JobMap   map[string]*JobSpec        // Quick lookup by name
    Schema   *jsonschema.Schema         // Validation schema
    Bindings *JobBinding                // Optional job binding
}
```

## Usage Examples

### Discover Available Models
```bash
$ ./liteci models
Available Models:

  charts
  helm
  helmCommon
  terraform
```

### View Model Details with Multiple Jobs
```bash
$ ./liteci model terraform

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Model: terraform
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Description:
  Model: terraform

Job Information:
  Name:        plan
  Description: Plan Terraform configuration (and 2 more jobs available)
```

### Generate Plan
```bash
$ ./liteci plan --intent examples/intent.yaml

□ Loading intent...
□ Loading component types...
□ Normalizing intent...
□ Validating components against type schemas...
□ Expanding (env × component)...
□ Binding jobs and resolving dependencies...
□ Detecting cycles...
□ Topologically sorting...
□ Rendering plan...
✓ Plan generated with 10 jobs
```

## Available Jobs by Model

### Helm Model
- **deploy** (default): Deploy using Helm
- **rollback**: Rollback Helm release to previous version
- **diff**: Show differences between deployed and intended release

### Terraform Model
- **plan** (default): Plan Terraform configuration
- **apply**: Apply Terraform configuration
- **destroy**: Destroy Terraform managed infrastructure

### Charts Model
- **package** (default): Package and lint chart definitions
- **publish**: Publish charts to OCI registry
- **test**: Test chart templates and values

### HelmCommon Model
- **deploy** (default): Deploy common services using Helm
- **health-check**: Health check for common services
- **rollback**: Rollback common services to previous version

## Backward Compatibility

The system maintains backward compatibility:
- Plans still generate identical output
- The first job in each JobRegistry is used as the default
- Existing templates and step execution remain unchanged
- JobRegistry is constructed from individual model job.yaml files

## Future Extensions

The new structure enables:
1. **Conditional Jobs**: Jobs with platform or version constraints
2. **Job Dependencies**: Jobs that depend on other jobs
3. **Job Templating**: Sharing common job definitions
4. **Dynamic Job Selection**: Choosing jobs based on intent or environment
5. **Job Composition**: Building complex jobs from reusable steps
