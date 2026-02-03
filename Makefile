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
GOLANGCI_LINT_VERSION := 2.8.0

lint:
	@which golangci-lint > /dev/null || (echo "golangci-lint not found." && exit 1)
	@INSTALLED=$$(golangci-lint version --short | sed 's/^v//'); \
	if [ "$$INSTALLED" != "$(GOLANGCI_LINT_VERSION)" ]; then \
		echo "golangci-lint version mismatch: installed $$INSTALLED, expected $(GOLANGCI_LINT_VERSION)"; \
		exit 1; \
	fi
	golangci-lint run
