DOCKERHUB_USER ?= kwv4
IMAGE_NAME ?= tudomesh
# VERSION is the strict, exact tag for releases
VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null | sed 's/^v//' || git tag -l 'v*' | sort -V | tail -n1 | sed 's/^v//')

# DEV_VERSION gets a descriptive version for local builds (e.g., "1.2.3-1-gabcdef-dirty")
# Falls back to "local-dev" if no tags exist
DEV_VERSION := $(shell git describe --tags --dirty --always 2>/dev/null | sed 's/^v//' || echo "local-dev")

REMOTE_IMAGE := $(DOCKERHUB_USER)/$(IMAGE_NAME)

.PHONY: build build-dev test lint run clean docker-build bump bump-minor bump-major check-version show-version verify-release

# Build binary
build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o tudomesh .

# Build for local development with version info
build-dev:
	@echo "Building local binary for dev: $(DEV_VERSION)"
	CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$(DEV_VERSION)" -o tudomesh .
	@echo "Building dev image: $(IMAGE_NAME):$(DEV_VERSION)"
	docker build -t $(IMAGE_NAME):$(DEV_VERSION) .

# Run tests
test:
	go test -v ./...

# Run linter
lint:
	$(shell go env GOPATH)/bin/golangci-lint run

# Verify release config without publishing
verify-release:
	goreleaser release --snapshot --clean --skip=publish

# Run locally
run:
	go run . --config config.yaml

# Run with example config
run-example:
	go run . --config config.example.yaml

# Parse JSON exports (test mode)
parse:
	go run . --parse-only

# Clean build artifacts
clean:
	rm -f tudomesh
	-docker rmi $(IMAGE_NAME):$(DEV_VERSION) || true
	-docker rmi $(REMOTE_IMAGE):latest || true

# Docker build
docker-build:
	docker build -t $(REMOTE_IMAGE) .

# Docker compose up
up:
	docker-compose up -d

# Docker compose down
down:
	docker-compose down

# Version management
bump:
	$(call bump_version,patch)

bump-minor:
	$(call bump_version,minor)

bump-major:
	$(call bump_version,major)

check-version:
	@if [ -z "$(VERSION)" ]; then \
		echo "No Git tag found. Please tag your commit (e.g., git tag v1.0.0)"; \
		exit 1; \
	else \
		echo "Using version: $(VERSION)"; \
	fi

show-version:
	@echo "Release VERSION: $(VERSION)"
	@echo "Dev VERSION: $(DEV_VERSION)"


define bump_version
	@LATEST_TAG=$$(git tag -l 'v*' | sort -V | tail -n1); \
	if [ -z "$$LATEST_TAG" ]; then \
		echo "No existing tags found. Please create an initial tag first (e.g., git tag v1.0.0)"; \
		exit 1; \
	fi; \
	CURRENT_VERSION=$$(echo $$LATEST_TAG | sed 's/^v//'); \
	MAJOR=$$(echo $$CURRENT_VERSION | cut -d. -f1); \
	MINOR=$$(echo $$CURRENT_VERSION | cut -d. -f2); \
	PATCH=$$(echo $$CURRENT_VERSION | cut -d. -f3); \
	if [ "$(1)" = "patch" ]; then \
		NEW_PATCH=$$(expr $$PATCH + 1); \
		NEW_VERSION=$$MAJOR.$$MINOR.$$NEW_PATCH; \
	elif [ "$(1)" = "minor" ]; then \
		NEW_MINOR=$$(expr $$MINOR + 1); \
		NEW_VERSION=$$MAJOR.$$NEW_MINOR.0; \
	elif [ "$(1)" = "major" ]; then \
		NEW_MAJOR=$$(expr $$MAJOR + 1); \
		NEW_VERSION=$$NEW_MAJOR.0.0; \
	fi; \
	NEW_TAG=v$$NEW_VERSION; \
	BRANCH_NAME=release-bump-$$(date +%Y%m%d-%H%M%S); \
	echo "Creating release branch and bumping version from $$LATEST_TAG to $$NEW_TAG"; \
	git checkout -b $$BRANCH_NAME; \
	git commit --allow-empty -m "chore: release $$NEW_TAG"; \
	git tag -a $$NEW_TAG -m "Release $$NEW_TAG"; \
	git push origin HEAD --follow-tags; \
	gh pr create --fill --base main --title "chore: release $$NEW_TAG"
endef
