COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

default: build

.PHONY: build
build:
	@go build -trimpath -ldflags "-s -w -X github.com/jamesl33/hive-cli/cmd.commit=$(COMMIT)" -o build/hive

.PHONY: install
install:
	@go build -ldflags "-X github.com/jamesl33/hive-cli/cmd.commit=$(COMMIT)" -o $(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)/hive

.PHONY: test
test:
	@go test ./... -count=1 -covermode=atomic -coverprofile=coverage.out -failfast -shuffle=on

.PHONY: coverage
coverage: test
	@go tool cover -html=coverage.out

.PHONY: clean
clean:
	@rm -rf build coverage.out
