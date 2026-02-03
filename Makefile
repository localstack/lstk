BINARY_NAME=lstk
BUILD_DIR=bin

.PHONY: build clean test test-integration lint mock-generate

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) .

clean:
	rm -rf $(BUILD_DIR)

test:
	go test ./internal/...

test-integration: build
	cd test/integration && go test -count=1 -v .

mock-generate:
	go generate ./...

lint:
	@EXPECTED=$$(awk '/^golangci-lint/ {print $$2}' .tool-versions); \
	INSTALLED=$$(golangci-lint version --short 2>/dev/null | sed 's/^v//'); \
	[ "$$INSTALLED" = "$$EXPECTED" ] || { echo "golangci-lint $$EXPECTED required (found: $$INSTALLED)"; exit 1; }
	golangci-lint run
