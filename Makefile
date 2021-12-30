PROJECT_NAME := "tkestack.io/kvass"
PKG := "./"
PKG_LIST := $(shell go list ${PKG}/... | grep -v /vendor/)
GO_FILES := $(shell find . -name '*.go' | grep -v /vendor/ | grep -v _test.go)
GOOS := "linux"
GOARCH := "amd64"
.PHONY: all dep lint vet test test-coverage build clean

all: test-coverage lint vet  build

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
	@GOOS=${GOOS} GOARCH=${GOARCH} go build -o kvass cmd/kvass/*.go

clean: ## Remove previous build
	@rm -fr kvass
	@rm -fr cover.out coverage.txt
clog:
	@git-chglog -o CHANGELOG.md
