# Schema Documentation

## Overview

Liteci uses JSON Schema v5 to validate all input and output documents. Three schemas define the structure:

```
schemas/
├── intent.schema.json      # Validates intent.yaml
├── jobs.schema.json        # Validates jobs.yaml
└── plan.schema.json        # Validates plan.json output
```

## Loading Schemas

Schemas are loaded from the `schemas/` directory using the `Validator` in `internal/schema/validator.go`:

```go
// Create validator with schemas directory
validator, err := schema.NewValidator("./schemas")
if err != nil {
    return err
}

// Validate intent
if err := validator.ValidateIntent(intentData); err != nil {
    return fmt.Errorf("intent validation failed: %w", err)
}
```

## Intent Schema (`intent.schema.json`)

Defines structure for `intent.yaml`.

### Required Fields

- `apiVersion`: Must match `sourceplane.io/v[0-9]+`
- `kind`: Must be `"Intent"`
- `metadata.name`: Component name (1-63 chars, lowercase alphanumeric + hyphen)
- `components`: Array of component definitions

### Optional Fields

- `metadata.description`: Human-readable description
- `metadata.namespace`: Kubernetes namespace (default: "default")
- `groups`: Policy domains
- `forEach`: Environment definitions

### Example

```yaml
apiVersion: sourceplane.io/v1
kind: Intent

metadata:
  name: my-deployment
  description: Deployment specification

groups:
  platform:
    policies:
      isolation: strict
    defaults:
      namespace: platform

forEach:
  production:
    selectors:
      components: [web-app]
    defaults:
      replicas: 3

components:
  - name: web-app
    type: helm
```

## Job Registry Schema (`jobs.schema.json`)

Defines structure for `jobs.yaml`.

### Required Fields

- `apiVersion`: Must match `sourceplane.io/v[0-9]+`
- `kind`: Must be `"JobRegistry"`
- `jobs.<type>.name`: Job name
- `jobs.<type>.steps`: Array of execution steps

### Job Step Fields

- `name`: Step identifier
- `run`: Shell command (supports `{{.Variable}}` templates)
- `timeout`: Optional duration (e.g., "5m", "30s")
- `retry`: Optional retry count (0-10, default: 0)
- `onFailure`: Error handling ("stop" or "continue", default: "stop")

### Example

```yaml
apiVersion: sourceplane.io/v1
kind: JobRegistry

jobs:
  helm:
    name: deploy
    timeout: 15m
    retries: 2
    steps:
      - name: deploy
        run: helm upgrade --install {{.Component}} {{.chart}}
        timeout: 10m
        onFailure: stop
```

## Plan Schema (`plan.schema.json`)

Defines structure for `plan.json` output.

### Required Fields

- `apiVersion`: Must be `"v1"`
- `kind`: Must be `"Workflow"`
- `metadata.name`: Plan name
- `jobs`: Array of executable jobs

### Job ID Format

Job IDs follow pattern: `component@environment.jobname`

Example: `web-app@production.deploy`

### Plan Job Fields

- `id`: Unique job identifier
- `name`: Job name
- `component`: Component name
- `environment`: Environment name
- `type`: Component type
- `steps`: Array of execution steps (fully rendered)
- `env`: Merged configuration
- `dependsOn`: Job IDs this job depends on
- `labels`: Key-value labels
- `config`: Full configuration object

### Example

```json
{
  "apiVersion": "v1",
  "kind": "Workflow",
  "metadata": {
    "name": "my-deployment"
  },
  "jobs": [
    {
      "id": "web-app@production.deploy",
      "name": "deploy",
      "component": "web-app",
      "environment": "production",
      "type": "helm",
      "steps": [
        {
          "name": "deploy",
          "run": "helm upgrade --install web-app oci://... --replicas 3",
          "timeout": "10m",
          "onFailure": "stop"
        }
      ],
      "dependsOn": ["common-services@production.deploy"],
      "env": {
        "replicas": 3,
        "chart": "oci://..."
      }
    }
  ]
}
```

## Schema Loading in Pipeline

The schema validation integrates into the compilation pipeline:

```
1. Load Files
   └─ intent.yaml, jobs.yaml

2. Validate Against Schemas
   ├─ ValidateIntent(intent) → Check against intent.schema.json
   └─ ValidateJobRegistry(jobs) → Check against jobs.schema.json

3. Compile Pipeline
   ├─ Normalize
   ├─ Expand
   ├─ Plan
   └─ Render

4. Output Validation
   └─ ValidatePlan(plan) → Check against plan.schema.json
```

## Schema File Locations

When using the CLI, schemas are loaded from:

```
./schemas/
  ├── intent.schema.json
  ├── jobs.schema.json
  └── plan.schema.json
```

You can specify a custom schemas directory:

```go
validator, err := schema.NewValidator("/path/to/schemas")
```

## Validation Errors

If a document violates the schema, a detailed error is returned:

```
intent validation failed: jsonschema validation error:
- component "web-app" is missing required property "type"
- component "web-app" must have maxLength: 63, but got length 100
```

## Extending Schemas

To add new validation rules:

1. Update the appropriate schema file (intent/jobs/plan)
2. Add the new field definition
3. Update documentation
4. Test with examples

Example: Add new policy field

```json
{
  "type": "object",
  "properties": {
    "policies": {
      "type": "object",
      "properties": {
        "my-policy": {
          "type": "string",
          "enum": ["value1", "value2"]
        }
      }
    }
  }
}
```

## Current Validation Coverage

### Intent Schema
✓ API version format  
✓ Kind validation  
✓ Metadata requirements  
✓ Component name patterns  
✓ Dependency structure  
✓ Policy/defaults separation  

### Jobs Schema
✓ Job type definitions  
✓ Step requirements  
✓ Timeout format  
✓ Retry bounds  

### Plan Schema
✓ Job ID format  
✓ Required job fields  
✓ Step structure  
✓ Dependency references  

## Future Enhancements

- [ ] Custom error messages
- [ ] Validation warnings (in addition to errors)
- [ ] Cross-document validation (e.g., component types match job registry)
- [ ] Schema versioning
- [ ] Dynamic schema loading from URLs
