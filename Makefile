BINARY_NAME=lstk
BUILD_DIR=bin

.PHONY: build clean test-integration

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) .

clean:
	rm -rf $(BUILD_DIR)

test-integration: build
	cd test/integration && go test -count=1 -v .
