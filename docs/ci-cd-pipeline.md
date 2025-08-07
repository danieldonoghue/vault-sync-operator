# CI/CD Pipeline Documentation

This document describes the GitHub Actions CI/CD pipeline for the vault-sync-operator.

## Overview

The CI/CD pipeline consists of several workflows that handle testing, building, security scanning, and releasing:

- **CI Workflow** (`ci.yaml`) - Runs on every push and PR
- **Release Workflow** (`release.yaml`) - Triggers on version tags
- **Security Workflow** (`security.yaml`) - Runs security scans
- **Dependabot** (`dependabot.yml`) - Automated dependency updates

## CI Workflow

### Triggers
- Push to `master` or `develop` branches
- Pull requests to `master` branch

### Jobs

#### 1. Test
- Runs unit tests with race detection
- Generates code coverage reports
- Uploads coverage to Codecov

#### 2. Lint
- Runs `golangci-lint` with comprehensive linter configuration
- Ensures code quality and consistency

#### 3. Security Scan
- Runs Gosec security scanner
- Uploads SARIF results to GitHub Security tab

#### 4. Build Test
- Tests cross-compilation for multiple platforms:
  - Linux (amd64, arm64)
  - macOS (amd64, arm64)
  - Windows (amd64)

#### 5. Docker Test
- Tests multi-architecture Docker builds
- Validates Dockerfile syntax and build process

## Release Workflow

### Triggers
- Push of version tags (format: `v*`)

### Process

#### 1. Create Release
- Extracts version from git tag
- Generates changelog from commit history
- Creates GitHub release with generated notes

#### 2. Build Binaries
- Cross-compiles binaries for multiple platforms
- Embeds version information using ldflags
- Creates compressed archives (tar.gz for Unix, zip for Windows)
- Generates SHA256 checksums
- Uploads all artifacts to GitHub release

#### 3. Build and Push Docker Images
- Builds multi-architecture Docker images (amd64, arm64)
- Pushes to GitHub Container Registry (ghcr.io)
- Uses semantic versioning tags:
  - `v1.2.3` (exact version)
  - `v1.2` (minor version)
  - `v1` (major version)
  - `latest` (for stable releases)
- Generates Software Bill of Materials (SBOM)

#### 4. Update Manifests
- Updates Kubernetes manifests with new image version
- Creates versioned release manifests and packages them as GitHub release assets
- Packages Helm charts as GitHub release assets

## Security Workflow

### Triggers
- Weekly schedule (Sundays)
- Push to `master` branch
- Pull requests

### Scans
- **Trivy**: Vulnerability scanning for filesystem and Docker images
- **Dependency Review**: Reviews dependencies in PRs for security issues
- **Gosec**: Go-specific security analysis

## Container Registry

Images are published to GitHub Container Registry:

```
ghcr.io/danieldonoghue/vault-sync-operator:latest
ghcr.io/danieldonoghue/vault-sync-operator:v1.0.0
ghcr.io/danieldonoghue/vault-sync-operator:v1.0
ghcr.io/danieldonoghue/vault-sync-operator:v1
```

### Multi-Architecture Support

All Docker images support:
- `linux/amd64`
- `linux/arm64`

## Binary Releases

For each release, the following binary artifacts are available:

- `vault-sync-operator-linux-amd64.tar.gz`
- `vault-sync-operator-linux-arm64.tar.gz`
- `vault-sync-operator-darwin-amd64.tar.gz`
- `vault-sync-operator-darwin-arm64.tar.gz`
- `vault-sync-operator-windows-amd64.zip`

Each archive includes:
- The compiled binary
- README.md
- LICENSE
- SHA256 checksum

## Version Information

Binaries and Docker images include embedded version information:
- Version (from git tag)
- Commit SHA
- Build date

Access version info:
```bash
./vault-sync-operator --version
```

## Release Process

### Creating a Release

1. **Prepare the release:**
   ```bash
   git checkout main
   git pull origin main
   ```

2. **Create and push a version tag:**
   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```

3. **Monitor the release workflow:**
   - Check GitHub Actions for workflow progress
   - Verify artifacts are uploaded correctly
   - Confirm Docker images are published

### Pre-release Versions

For pre-release versions (alpha, beta, rc):
```bash
git tag v1.0.0-alpha.1
git push origin v1.0.0-alpha.1
```

Pre-release versions are marked as "prerelease" in GitHub and don't update the `latest` tag.

## Development Workflow

### Branch Protection

The `master` branch should be protected with:
- Required status checks for all CI jobs
- Require branches to be up to date
- No direct pushes (require PRs)

### PR Requirements

Before merging PRs:
- All CI checks must pass
- Code coverage should not decrease
- Security scans must pass
- Manual review required

## Troubleshooting

### Common Issues

#### Build Failures
- Check Go version compatibility
- Verify all dependencies are available
- Ensure cross-compilation works locally

#### Docker Build Issues
- Verify Dockerfile syntax
- Check multi-arch build compatibility
- Ensure base images are available

#### Release Issues
- Verify tag format (`v*`)
- Check GitHub token permissions
- Ensure GHCR access is configured

### Debug Commands

Test the build locally:
```bash
# Test Go build
go build -ldflags="-X main.version=test" cmd/main.go

# Test Docker build
docker buildx build --platform linux/amd64,linux/arm64 .

# Test cross-compilation
GOOS=linux GOARCH=arm64 go build cmd/main.go
```

## Security Considerations

- GitHub tokens have minimal required permissions
- Container images use distroless base for security
- SBOM generation for supply chain transparency
- Regular dependency updates via Dependabot
- Automated security scanning

## Monitoring

Monitor the CI/CD pipeline health:
- GitHub Actions dashboard
- Failed workflow notifications
- Security advisory alerts
- Dependabot PR reviews
