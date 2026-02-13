BINARY_NAME=lstk
ifeq ($(OS),Windows_NT)
    BINARY_NAME=lstk.exe
endif
BUILD_DIR=bin

.PHONY: build clean test test-integration lint mock-generate

$(BUILD_DIR)/$(BINARY_NAME):
	go build -o $(BUILD_DIR)/$(BINARY_NAME) .

build: clean $(BUILD_DIR)/$(BINARY_NAME)

clean:
	rm -rf $(BUILD_DIR)

test:
	@JUNIT=""; [ -n "$$CREATE_JUNIT_REPORT" ] && JUNIT="--junitfile test-results.xml"; \
	go run gotest.tools/gotestsum@latest --format testdox $$JUNIT -- ./cmd/... ./internal/...

test-integration: build
	@JUNIT=""; [ -n "$$CREATE_JUNIT_REPORT" ] && JUNIT="--junitfile ../../test-integration-results.xml"; \
	if [ "$$(uname)" = "Darwin" ]; then \
		cd test/integration && KEYRING=file go run gotest.tools/gotestsum@latest --format testdox $$JUNIT -- -count=1 ./...; \
	else \
		cd test/integration && go run gotest.tools/gotestsum@latest --format testdox $$JUNIT -- -count=1 ./...; \
	fi

mock-generate:
	go generate ./...

lint:
	@EXPECTED=$$(awk '/^golangci-lint/ {print $$2}' .tool-versions); \
	INSTALLED=$$(golangci-lint version --short 2>/dev/null | sed 's/^v//'); \
	[ "$$INSTALLED" = "$$EXPECTED" ] || { echo "golangci-lint $$EXPECTED required (found: $$INSTALLED)"; exit 1; }
	golangci-lint run --tests ./...
	(cd test/integration && golangci-lint run --tests ./...)
