# OCI Provider Package Specification

## Overview

The **lite-ci** provider is distributed as an OCI (Open Container Initiative) artifact, packaged with complete metadata, schemas, and runtime resources. This enables seamless installation and usage through OCI-compatible tools like ORAS and Thin.

## Provider Specification

The provider manifest is defined in [thin.provider.yaml](thin.provider.yaml):

```yaml
apiVersion: thin.io/v1
kind: Provider
metadata:
  name: lite-ci
  version: v0.1.0
  license: Apache-2.0
distribution:
  type: oci
  ref: ghcr.io/sourceplane/lite-ci
```

### Key Fields

| Field | Purpose |
|-------|---------|
| `metadata` | Provider identity, versioning, and authorship |
| `runtime` | Supported execution environments (native, docker, kubernetes, oras) |
| `entrypoint` | Binary name and default arguments for execution |
| `platforms` | Supported OS/arch combinations (linux/darwin, amd64/arm64) |
| `layers` | Bundled content organized by capability |
| `capabilities` | Exposed operations (plan, validate, debug) |
| `assets` | Static resources (profiles, schemas, defaults) |
| `architecture` | Implementation pattern and design principles |

## OCI Package Contents

The OCI artifact published to `ghcr.io/sourceplane/lite-ci:v0.1.0` contains:

### 1. **Provider Manifest** (`thin.provider.yaml`)
- Complete provider specification
- Metadata and distribution info
- Runtime environment declarations
- Capability definitions

### 2. **Binaries** (`bin/`)
```
bin/
├── linux/
│   ├── amd64/entrypoint
│   └── arm64/entrypoint
└── darwin/
    ├── amd64/entrypoint
    └── arm64/entrypoint
```
Multi-platform compiled binaries for direct execution

### 3. **Assets** (`assets/`)
```
assets/
├── config/
│   ├── schemas/           # Validation schemas
│   │   ├── intent.schema.yaml
│   │   ├── jobs.schema.yaml
│   │   └── plan.schema.yaml
│   └── compositions/      # Job definitions by type
│       ├── helm/
│       ├── terraform/
│       ├── charts/
│       └── helmCommon/
├── defaults/              # Default configuration
└── capabilities/          # Capability declarations
```

### 4. **Metadata Layers**

| Layer | Type | Content |
|-------|------|---------|
| `core` | Required | Provider manifest, schemas, composition definitions |
| `examples` | Optional | Use-case templates and reference workflows |

## Distribution Architecture

```
GitHub Release (v0.1.0)
├── liteci_0.1.0_linux_amd64.tar.gz    [minimal: binary only]
├── liteci_0.1.0_darwin_arm64.tar.gz   [minimal: binary only]
└── checksums.txt

GitHub Container Registry (GHCR)
├── ghcr.io/sourceplane/lite-ci:v0.1.0         [full OCI artifact]
│   ├── thin.provider.yaml
│   ├── bin/{os}/{arch}/entrypoint
│   ├── assets/schemas/**
│   ├── assets/compositions/**
│   └── assets/defaults/**
└── ghcr.io/sourceplane/lite-ci:v0.1.0-examples [optional examples layer]
```

## Installation Methods

### Via OCI Registry (Recommended)
```bash
oras pull ghcr.io/sourceplane/lite-ci:v0.1.0
```

### Via GitHub Release
```bash
# Download minimal binary
tar xzf liteci_0.1.0_linux_amd64.tar.gz
```

### Via Package Manager (Future)
```bash
thin provider install lite-ci@v0.1.0
```

## File Organization

- **Provider Definition**: [thin.provider.yaml](thin.provider.yaml)
- **Composition Specs**: [assets/config/compositions/](assets/config/compositions/)
- **Schemas**: [assets/config/schemas/](assets/config/schemas/)
- **CLI Entry**: [cmd/liteci/main.go](cmd/liteci/main.go)
- **Build Config**: [.goreleaser.yaml](.goreleaser.yaml)
- **Release Workflow**: [.github/workflows/release-oci.yaml](.github/workflows/release-oci.yaml)

## Release Process

1. **Tag Release**: `git tag v0.1.0 && git push origin v0.1.0`
2. **GitHub Actions Executes**:
   - Builds multi-platform binaries (entrypoint + liteci)
   - Creates dual archives:
     - OCI bundle: full metadata + binaries + schemas
     - GitHub release: binary only (minimal)
   - Pushes OCI artifact to GHCR
   - Creates GitHub release with checksums
3. **Available At**:
   - OCI: `ghcr.io/sourceplane/lite-ci:v0.1.0`
   - GitHub: Release page with binary downloads

## Entrypoint Binary

The OCI package includes the `entrypoint` executable, which is the provider's entry point:

```bash
# Usage
entrypoint -c assets/config/compositions plan -i intent.yaml -j jobs.yaml
```

Maps to the same interface as the `liteci` CLI:
- `plan` - Generate execution plan
- `validate` - Validate configurations  
- `debug` - Debug intent processing
- `compositions` - List available compositions

## Capabilities

The provider exposes three core capabilities:

### 1. Plan
Generate deterministic execution plans from intent specifications
- **Input**: intent.yaml + jobs.yaml
- **Output**: plan.json (DAG)
- **Stability**: Stable (v0.1.0+)

### 2. Validate
Validate intent and component definitions against schemas
- **Input**: intent.yaml + compositions
- **Output**: Validation report
- **Stability**: Stable (v0.1.0+)

### 3. Debug
Trace intent processing through expansion stages
- **Input**: intent.yaml
- **Output**: Processing trace
- **Stability**: Experimental (v0.1.0+)

## Asset Immutability

All bundled assets (schemas, compositions, defaults) are:
- ✅ Immutable within a provider version
- ✅ Co-versioned with provider binary
- ✅ Validated at provider release time
- ✅ Included in OCI artifact checksum

## Next Steps

- **First Release**: Run `make release-snapshot` locally to test GoReleaser config
- **Tag & Release**: `git tag v0.1.0 && git push origin v0.1.0`
- **Verify OCI**: `oras manifest fetch ghcr.io/sourceplane/lite-ci:v0.1.0`
- **Test Installation**: `thin provider install lite-ci@v0.1.0` (when thin is configured)
