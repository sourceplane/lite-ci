# Schema Structure Guide

## Schemas Directory Location

```
liteci/
└── schemas/
    ├── intent.schema.json      # Intent.yaml validation
    ├── jobs.schema.json        # Jobs.yaml validation
    └── plan.schema.json        # Plan.json output validation
```

## How Schemas Are Loaded

### 1. File Location
Schemas are placed in the `schemas/` directory at the project root:
- Relative path: `./schemas/`
- Each schema is a JSON Schema v5 file

### 2. Loading Mechanism
Schemas are loaded using the `Validator` in `internal/schema/validator.go`:

```go
package schema

type Validator struct {
    intentSchema *jsonschema.Schema
    jobsSchema   *jsonschema.Schema
    planSchema   *jsonschema.Schema
}

// Create validator
validator, err := schema.NewValidator("./schemas")

// Validate documents
validator.ValidateIntent(intentData)
validator.ValidateJobRegistry(jobsData)
validator.ValidatePlan(planData)
```

### 3. CLI Integration
The CLI uses schemas in the validate command:

```bash
./liteci validate -i intent.yaml -j jobs.yaml
```

This triggers schema validation on both files.

## Schema Files

### `intent.schema.json`
Validates `intent.yaml` documents.

**Key validations**:
- API version format: `sourceplane.io/v[0-9]+`
- Kind must be: `Intent`
- Component name: 1-63 chars, lowercase alphanumeric + hyphen
- Required fields: `apiVersion`, `kind`, `metadata`, `components`

**Location**: `schemas/intent.schema.json`  
**Size**: ~3.4 KB

### `jobs.schema.json`
Validates `jobs.yaml` documents.

**Key validations**:
- API version format: `sourceplane.io/v[0-9]+`
- Kind must be: `JobRegistry`
- Job name required
- Steps required (array of execution steps)
- Timeout format: `[0-9]+(m|h|s)`

**Location**: `schemas/jobs.schema.json`  
**Size**: ~2.0 KB

### `plan.schema.json`
Validates `plan.json` output.

**Key validations**:
- API version must be: `v1`
- Kind must be: `Workflow`
- Job ID format: `component@environment.jobname`
- Required job fields: `id`, `name`, `component`, `environment`, `type`, `steps`

**Location**: `schemas/plan.schema.json`  
**Size**: ~2.8 KB

## Loading Process

When liteci runs, schemas are loaded in this order:

```
1. Initialize Validator
   NewValidator("./schemas")
   ├─ Load schemas/intent.schema.json
   ├─ Load schemas/jobs.schema.json
   └─ Load schemas/plan.schema.json

2. During Validation Phase
   ├─ Validate intent.yaml → ValidateIntent()
   ├─ Validate jobs.yaml → ValidateJobRegistry()
   └─ Validate plan.json → ValidatePlan()

3. Error Handling
   └─ Return detailed schema violation messages
```

## Custom Schema Paths

To use schemas from a different location:

```go
// Go code
validator, err := schema.NewValidator("/custom/path/to/schemas")
if err != nil {
    return err
}
```

Or environment variable (future enhancement):

```bash
export LITECI_SCHEMAS_DIR=/custom/schemas
./liteci validate -i intent.yaml -j jobs.yaml
```

## Schema Validation Flow

```
intent.yaml
    ↓
Load intent data
    ↓
ValidateIntent(data)
    ↓
Compare against intent.schema.json
    ├─ Check apiVersion format ✓
    ├─ Check kind = "Intent" ✓
    ├─ Validate metadata ✓
    ├─ Validate components ✓
    └─ Validate groups/forEach ✓
    ↓
If valid: Continue compilation
If invalid: Return schema violation error
```

## Error Messages

Schema validation errors include:

```
intent validation failed: jsonschema validation error
- component "web-app" is missing required property "type"
- component "web-app" must have maxLength: 63
- metadata.name pattern does not match "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
```

## Adding New Schema Validations

### 1. Update Schema File

Edit the appropriate schema file (intent/jobs/plan) to add validation rules:

```json
{
  "properties": {
    "newField": {
      "type": "string",
      "enum": ["value1", "value2"],
      "pattern": "^[a-z]+$"
    }
  }
}
```

### 2. Test with Examples

Create test YAML files and validate:

```bash
./liteci validate -i test-intent.yaml -j test-jobs.yaml
```

### 3. Update Documentation

Add field to [SCHEMAS.md](SCHEMAS.md) with description and example.

## Dependency

Schemas require the JSON Schema v5 validator:

```go
import "github.com/santhosh-tekuri/jsonschema/v5"
```

This is included in `go.mod`:

```
require github.com/santhosh-tekuri/jsonschema/v5 v5.3.1
```

## Directory Structure Summary

```
liteci/
├── cmd/liteci/main.go
├── internal/
│   ├── schema/
│   │   └── validator.go        # Schema loading and validation
│   ├── model/
│   ├── loader/
│   ├── normalize/
│   ├── expand/
│   ├── planner/
│   └── render/
├── schemas/                    # ← Schema files location
│   ├── intent.schema.json      # Intent validation
│   ├── jobs.schema.json        # Jobs validation
│   └── plan.schema.json        # Plan validation
└── examples/
    ├── intent.yaml
    └── jobs.yaml
```

## Validation Checklist

When adding a new feature, validate:

- [ ] Add schema to appropriate file (intent/jobs/plan)
- [ ] Test with valid example
- [ ] Test with invalid example
- [ ] Update SCHEMAS.md documentation
- [ ] Build and test CLI: `go build && ./liteci validate`

## Current Implementation Status

✅ Schemas directory created  
✅ intent.schema.json implemented  
✅ jobs.schema.json implemented  
✅ plan.schema.json implemented  
✅ Validator module created (internal/schema/validator.go)  
✅ Schema validation integrated into loader  
✅ CLI validate command working  
✅ Error messages from schema violations  
