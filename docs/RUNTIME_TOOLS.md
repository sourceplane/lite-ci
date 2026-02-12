# Runtime Tools & OCI Image Usage

The `lite-ci` provider is distributed as an OCI-compliant artifact that can be consumed by multiple container runtimes and tools.

## Available Runtimes

### 1. **Native (Default)**
Direct binary execution without containerization.

```bash
# Extract binary for your platform
oras pull ghcr.io/sourceplane/lite-ci:v0.1.0
tar -xzf lite-ci_v0.1.0_linux_amd64_oci.tar.gz
./entrypoint plan -i intent.yaml
```

### 2. **Docker**
Traditional Docker container runtime.

```bash
docker run \
  -v $(pwd):/workspace \
  ghcr.io/sourceplane/lite-ci:v0.1.0 \
  entrypoint plan -i /workspace/intent.yaml
```

### 3. **Podman** (Docker-Compatible)
Drop-in replacement for Docker, fully OCI-compliant.

```bash
podman run \
  -v $(pwd):/workspace \
  ghcr.io/sourceplane/lite-ci:v0.1.0 \
  entrypoint plan -i /workspace/intent.yaml
```

**Advantages:**
- Daemonless (rootless option available)
- Better for CI/CD pipelines
- No Docker daemon required

### 4. **containerd** (nerdctl)
High-performance container runtime used by Kubernetes.

```bash
nerdctl run \
  -v $(pwd):/workspace \
  ghcr.io/sourceplane/lite-ci:v0.1.0 \
  entrypoint plan -i /workspace/intent.yaml
```

**Or using ctr directly:**
```bash
ctr run --mount type=bind,src=$(pwd),dst=/workspace,options=rbind:rw \
  ghcr.io/sourceplane/lite-ci:v0.1.0 \
  lite-ci-plan
```

### 5. **ORAS** (OCI Registry As Storage)
For artifact-centric workflows without running containers.

```bash
# Pull the entire provider artifact
oras pull ghcr.io/sourceplane/lite-ci:v0.1.0

# Extract specific components
oras pull ghcr.io/sourceplane/lite-ci:v0.1.0 \
  --output . \
  --include-config

# List contents
oras manifest fetch ghcr.io/sourceplane/lite-ci:v0.1.0
```

### 6. **Kubernetes**
Deploy as a Kubernetes Job or Pod.

```bash
kubectl run lite-ci \
  --image=ghcr.io/sourceplane/lite-ci:v0.1.0 \
  --image-pull-policy=IfNotPresent \
  -- entrypoint plan -i /workspace/intent.yaml
```

**Or using Helm:**
```bash
helm install lite-ci oci://ghcr.io/sourceplane/charts/lite-ci:v0.1.0
```

## OCI Image Inspection

### Using Docker/Podman
```bash
docker inspect ghcr.io/sourceplane/lite-ci:v0.1.0
```

### Using Skopeo
```bash
skopeo inspect docker://ghcr.io/sourceplane/lite-ci:v0.1.0
```

### Using ORAS
```bash
oras manifest fetch ghcr.io/sourceplane/lite-ci:v0.1.0
oras manifest fetch-config ghcr.io/sourceplane/lite-ci:v0.1.0
```

## Installation Methods

### Option 1: Via Package Managers (Native Binary)
```bash
# Homebrew (macOS)
brew install sourceplane/lite-ci/lite-ci

# APT (Debian/Ubuntu)
sudo apt-get install lite-ci

# YUM (CentOS/RHEL)
sudo yum install lite-ci
```

### Option 2: Direct Binary Download
```bash
# Download for your platform
wget https://github.com/sourceplane/lite-ci/releases/download/v0.1.0/liteci_v0.1.0_linux_amd64.tar.gz
tar -xzf liteci_v0.1.0_linux_amd64.tar.gz
sudo mv entrypoint /usr/local/bin/lite-ci
```

### Option 3: OCI Image Pull
```bash
# Docker
docker pull ghcr.io/sourceplane/lite-ci:v0.1.0

# Podman
podman pull ghcr.io/sourceplane/lite-ci:v0.1.0

# containerd
nerdctl pull ghcr.io/sourceplane/lite-ci:v0.1.0
```

### Option 4: ORAS Pull
```bash
oras pull ghcr.io/sourceplane/lite-ci:v0.1.0
```

## Comparison Matrix

| Tool | Container | Rootless | CI/CD Ready | Kubernetes | Binary | Notes |
|------|-----------|----------|------------|-----------|--------|-------|
| Docker | ✓ | ✗ | ✓ | ✓ | - | Industry standard, daemon required |
| Podman | ✓ | ✓ | ✓✓ | ✓ | - | Best for CI/CD, daemonless |
| containerd | ✓ | ✓ | ✓✓ | ✓✓ | - | K8s native, high performance |
| nerdctl | ✓ | ✓ | ✓✓ | ✓✓ | - | containerd CLI, Docker-compatible |
| ORAS | ✗ | ✓ | ✓ | ✓ | - | Artifact registry, no execution |
| Kubernetes | ✓ | ✓ | ✓✓ | ✓✓ | - | Orchestration platform |
| Native | ✗ | ✓ | ✓✓ | ✗ | ✓ | Direct binary, no container |

## Best Practices

1. **For CI/CD**: Use **Podman** or **containerd** (no daemon, better security)
2. **For Local Dev**: Use **Docker** or **Podman** (convenience)
3. **For Kubernetes**: Use **nerdctl** or **Kubernetes** manifests (native)
4. **For Distribution**: Use **ORAS** (artifact-centric)
5. **For Maximum Compatibility**: Use **native binary** (no runtime dependency)

## Environment Variables

When running in any container runtime, pass configuration via environment variables:

```bash
docker run \
  -e LITE_CI_LOG_LEVEL=debug \
  -e LITE_CI_CONFIG_DIR=/etc/lite-ci \
  ghcr.io/sourceplane/lite-ci:v0.1.0
```

## Troubleshooting

### "Image not found"
```bash
# Verify authentication
podman login ghcr.io
podman pull ghcr.io/sourceplane/lite-ci:v0.1.0
```

### "Cannot run container"
```bash
# Check image architecture
skopeo inspect docker://ghcr.io/sourceplane/lite-ci:v0.1.0 | grep architecture
```

### "Permission denied"
```bash
# For Podman rootless mode
podman run --userns=keep-id ghcr.io/sourceplane/lite-ci:v0.1.0
```
