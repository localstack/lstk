BINARY_NAME=lstk
BUILD_DIR=bin

.PHONY: build clean

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) .

clean:
	rm -rf $(BUILD_DIR)
