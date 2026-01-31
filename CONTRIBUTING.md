# Contributing to **tudomesh**

Thanks for your interest in contributing to **tudomesh**!
This guide explains how to develop, build, test, and release new
versions of the project.

------------------------------------------------------------------------

## Development Setup

### 1. Clone the Repository

``` sh
git clone https://github.com/kwv/tudomesh.git
cd tudomesh
```

### 2. Fast Local Development

``` sh
make build-dev
```

This compiles the binary locally and builds a fresh Docker image for `amd64` in seconds.

### 3. Linting and Verification

``` sh
make lint
make verify-release
```

- `make lint`: Runs `golangci-lint`.
- `make verify-release`: Runs a "snapshot" version of the GoReleaser pipeline locally to verify your configuration without publishing anything.

------------------------------------------------------------------------

## Releasing a New Version

The project uses **Git tags** to trigger automated Docker image builds
and releases.

### Steps to Publish a New Version

1.  Ensure your changes are committed to `main`.
2.  Choose the version increment type:

``` sh
make bump        # Patch release (v1.2.3 -> v1.2.4)
make bump-minor  # Minor release (v1.2.3 -> v1.3.0)
make bump-major  # Major release (v1.2.3 -> v2.0.0)
```

This automatically updates the version, tags the commit, and pushes the tag to GitHub.

### CI/CD Automation

The GitHub Actions workflow triggers on every push and pull request to ensure the code remains clean:

- **Push to main / PRs**: Runs `go test` and `golangci-lint`.
- **Tag Push**: Decodes the version from the tag, runs tests/lint, and then triggers **GoReleaser** to build and publish:
    - GitHub Release binaries (Linux, Windows, Darwin for `amd64` and `arm64`)
    - Multi-arch Docker images (`kwv4/tudomesh:v1.2.3` and `latest`)

### Tag Cleanup

The repository contains a weekly automated cleanup job (`docker-cleanup.yml`) that:
- Synchronizes Git tags and Docker Hub tags.
- Keeps the `latest` tag and the most recent semantic version tags (configured via `KEEP_RECENT`).
- Removes orphaned or old version tags from both systems.

------------------------------------------------------------------------

## Testing Locally

To test the image before publishing:

``` sh
docker build -t tudomesh:test .
docker run --rm -p 8080:8080 tudomesh:test --http --http-port 8080
```

------------------------------------------------------------------------

## Contribution Workflow

1. Create a branch for your changes (e.g., `feature/your-feature-name` or `bugfix/your-bug-name`).
2. Commit your changes to this branch.
3. Open a pull request (PR) targeting the `main` branch.
4. Ensure your PR includes a clear description of the changes.
5. Add any relevant labels or assignees as needed.

------------------------------------------------------------------------

## Required Secrets for CI

Add these secrets in your GitHub repository settings:

| Secret Name | Value |
|----------------------|---------------------------|
| `DOCKERHUB_USER` | `kwv4` |
| `DOCKERHUB_TOKEN` | *Docker Hub access token* |

------------------------------------------------------------------------

## Questions?

Feel free to open an issue or start a discussion if you have questions,
improvement ideas, or suggestions.
We welcome contributions of all kinds!
