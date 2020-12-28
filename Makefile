PKG := "./"
PKG_LIST := $(shell go list ${PKG}/... | grep -v /vendor/)
GO_FILES := $(shell find . -name '*.go' | grep -v /vendor/ | grep -v _test.go)

VERSION :=v0.0.3
BUILD_TIME := $(shell date +%Y%m%d%H%M%S)
GIT_COMMIT=$(shell git rev-parse HEAD)

BUILD_VERSION := $(BUILD_TIME).$(GIT_COMMIT)
IMAGE_NAME = woshiaotian/kvass:$(VERSION).$(BUILD_VERSION)

.PHONY: all dep lint vet test test-coverage build clean build-linux image

all: build

dep: ## Get the dependencies
	@go mod download

lint: ## Lint Golang files
	@golint -set_exit_status ${PKG_LIST}

vet: ## Run go vet
	@go vet ${PKG_LIST}

test: ## Run unittests
	@go test -short ${PKG_LIST}

test-coverage: ## Run tests with coverage
	@go test -short -coverprofile cover.out -covermode=atomic ${PKG_LIST}
	@cat cover.out >> coverage.txt

build: dep ## Build the binary file
	@go build -i -o kvass cmd/kvass/*.go

build-linux:
	@echo "building ${BIN_NAME} ${VERSION}"
	@echo "GOPATH=${GOPATH}"
    @CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -i -o kvass cmd/kvass/*.go

image: build-linux
	@docker build -f ./Dockerfile --rm -t ${IMAGE_NAME} .


clean: ## Remove previous build
	@rm -fr kvass
	@rm -fr cover.out coverage.txt
