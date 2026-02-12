# Model Display - Smart Job Expansion with --expand-jobs Flag

## Overview

The model display has been updated to be **cleaner by default** while providing **full details on demand** with the `--expand-jobs` flag.

## Key Changes

### Default Behavior (Without --expand-jobs)
```bash
$ ./liteci model helm
```

Shows:
- ✅ Component Model name
- ✅ Description
- ✅ **JobRegistry Binding** (registry name, description, default job, total jobs count)
- ✅ **Available Jobs** (list with scope, description, steps count, timeout)
- ❌ NO "Using Default Job" section
- ❌ NO job steps, required fields, or input fields

### With --expand-jobs Flag
```bash
$ ./liteci model helm --expand-jobs
```

Shows everything including:
- ✅ JobRegistry Binding and Available Jobs (as above)
- ✅ **Using Default Job** section
- ✅ Job Information (name, description)
- ✅ Required Fields
- ✅ Supported Input Fields
- ✅ Full Job Steps with commands, timeouts, and retries

---

## Command Examples

### Quick Model Overview
```bash
$ ./liteci model charts

Component Model: charts

JobRegistry Binding:
  Registry Name: charts-jobs
  Registry Desc: Helm charts packaging and publishing jobs
  Default Job:   package
  Total Jobs:    3

Available Jobs (from JobRegistry):
★ 1. package [packaging]
     Description: Package chart definitions
     Steps: 2 | Timeout: 5m

  2. publish [publishing]
     Description: Publish charts to registry
     Steps: 4 | Timeout: 10m

  3. test [testing]
     Description: Test chart templates and values
     Steps: 2 | Timeout: 5m
```

### Full Model Details with Jobs
```bash
$ ./liteci model charts --expand-jobs

[Shows JobRegistry and Available Jobs as above, plus:]

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Using Default Job: package
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Job Information:
  Name:        package
  Description: Package chart definitions

Required Fields:
  • name
  • type
  • inputs

Supported Input Fields:
  • inputs               - Chart packaging input values
  • name                 - Component name
  • type                 - Component type identifier

Job Steps (for package job):
  1. lint
     Timeout: 5m
     Command: helm lint {{.registry}}/charts

  2. package
     Timeout: 5m
     Command: helm package {{.registry}}/charts -d dist/
```

### List All Models (Long Format)
```bash
$ ./liteci models list -l

[Shows all models with JobRegistry and Available Jobs - no job details]
```

### List All Models with Full Details
```bash
$ ./liteci models list -l --expand-jobs

[Shows all models including job steps and field details]
```

---

## Flag Details

### `--expand-jobs` / `-e`

- **Location**: Available on both `liteci model` and `liteci models list` commands
- **Default**: false (don't expand)
- **Effect**: Shows the "Using Default Job" section with full job details
- **Use Case**: When you need to see:
  - Exact job steps and commands
  - Required fields
  - Supported input fields
  - Step timeouts and retries

---

## Display Sections

### Always Shown
1. **Component Model** - Model name (header)
2. **Description** - Model description
3. **JobRegistry Binding** - Registry info and job count
4. **Available Jobs** - List of all jobs with metadata

### Only with --expand-jobs
5. **Using Default Job** - Separator marking job details section
6. **Job Information** - Name and description of default job
7. **Required Fields** - Fields that must be provided
8. **Supported Input Fields** - Fields that can be configured
9. **Job Steps** - Full execution steps with commands

---

## Benefits

✅ **Clean by Default**: Quick overview without noise
✅ **Discoverable Jobs**: See all available jobs at a glance
✅ **Opt-in Details**: Full information available when needed
✅ **Consistent Naming**: JobRegistry and Job concepts clearly separated
✅ **Scope Labels**: Jobs are categorized (deployment, recovery, analysis, etc)
✅ **Metadata Rich**: Step counts, timeouts, and scopes visible immediately

---

## Use Cases

### Quick Check: "What models are available?"
```bash
$ ./liteci models
```
Shows list of 4 models

### Quick Check: "What jobs does helm have?"
```bash
$ ./liteci model helm
```
Shows 3 available jobs (deploy, rollback, diff) without step details

### Detailed Investigation: "Show me the deploy job steps"
```bash
$ ./liteci model helm --expand-jobs
```
Shows full job details including all steps

### Documentation: "List all models with all their jobs"
```bash
$ ./liteci models list -l --expand-jobs
```
Shows every model with every job and its full details

---

## Implementation Details

### Flag Additions
- Added `expandJobs` boolean variable
- Added `--expand-jobs` / `-e` flag to:
  - `liteci model [name]` command
  - `liteci models list` command

### Display Logic
- `PrintLongFormat()` now takes `expandJobs` parameter
- If `expandJobs` is false, function returns after showing Available Jobs section
- If `expandJobs` is true, continues to show full job details

### No Breaking Changes
- Default behavior is backward compatible
- Plans generation unaffected
- Existing scripts continue to work
- Only adds optional verbosity

---

## Summary

The model display is now **smarter and more user-friendly**:

- **Default**: Clean, focused display of JobRegistry and available jobs
- **With --expand-jobs**: Complete details for power users and documentation

Users can quickly discover available jobs, then use `--expand-jobs` for full details when needed.
