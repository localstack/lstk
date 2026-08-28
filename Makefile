BINARY_NAME=lstk
ifeq ($(OS),Windows_NT)
    BINARY_NAME=lstk.exe
endif
BUILD_DIR=bin
export CGO_ENABLED=0

.PHONY: build clean test test-integration test-scripts lint govulncheck mock-generate otel

# Always invoke `go build` and let Go's build cache handle incrementality; a
# file target on bin/lstk would be skipped when the binary exists, even with
# stale sources.
build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) .

clean:
	rm -rf $(BUILD_DIR)

test:
	@JUNIT=""; [ -n "$$CREATE_JUNIT_REPORT" ] && JUNIT="--junitfile test-results.xml"; \
	go run gotest.tools/gotestsum@latest --format testname $$JUNIT -- ./cmd/... ./internal/...

test-integration: build
	@RUN="$(RUN)" ./scripts/test-integration.sh

# Bash suites for the release helper scripts under scripts/. They only ever run
# on the Linux release runner, so a bash suite is the faithful test here.
test-scripts:
	@./scripts/test-scripts.sh

otel:
	docker compose -f docker-compose.tracing.yaml up -d

mock-generate:
	go generate ./...

lint:
	@EXPECTED=$$(awk '/^golangci-lint/ {print $$2}' .tool-versions); \
	INSTALLED=$$(golangci-lint version --short 2>/dev/null | sed 's/^v//'); \
	[ "$$INSTALLED" = "$$EXPECTED" ] || { echo "golangci-lint $$EXPECTED required (found: $$INSTALLED)"; exit 1; }
	golangci-lint run --tests ./...
	(cd test/integration && golangci-lint run --tests ./...)

govulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	(cd test/integration && go run golang.org/x/vuln/cmd/govulncheck@latest ./...)
