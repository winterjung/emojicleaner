GOPATH:=$(shell go env GOPATH)
APP?=emojicleaner

.PHONY: build
## build: build the application
build:
	go build -o build/${APP} ./cmd/download
	go build -o build/${APP} ./cmd/favorite
	go build -o build/${APP} ./cmd/longest
	go build -o build/${APP} ./cmd/popular
	go build -o build/${APP} ./cmd/stale

.PHONY: format
## format: format files
format:
	@go install github.com/incu6us/goimports-reviser/v2@latest
	goimports-reviser -file-path $$(find . -name "*.go") -rm-unused
	gofmt -s -w .
	go mod tidy

.PHONY: test
## test: run tests
test:
	@go install github.com/rakyll/gotest@latest
	gotest -race -cover ./...

.PHONY: coverage
## coverage: run tests with coverage
coverage:
	@go install github.com/rakyll/gotest@latest
	gotest -race -coverprofile=coverage.txt -covermode=atomic ./...

.PHONY: lint
## lint: check everything's okay
lint:
	golangci-lint run ./...
	go mod verify

.PHONY: help
## help: prints this help message
help:
	@echo "Usage: \n"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':'
