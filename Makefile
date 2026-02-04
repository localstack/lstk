BINARY_NAME=lstk
BUILD_DIR=bin

.PHONY: build clean test test-integration mock-generate

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
