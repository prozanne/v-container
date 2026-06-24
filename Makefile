# v-container build

VERSION ?= dev
LDFLAGS := -s -w -X github.com/prozanne/v-container/internal/cli.Version=$(VERSION)
PKG := ./cmd/vc

.PHONY: all build build-windows build-host test vet fmt clean

all: build

## build: produce the Windows exe and a host binary in dist/
build: build-windows build-host

build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/vc.exe $(PKG)

build-host:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/vc $(PKG)

## test: run unit tests
test:
	go test ./... -count=1

## vet: run go vet
vet:
	go vet ./...

## fmt: format all Go sources
fmt:
	gofmt -w ./internal ./cmd

clean:
	rm -rf dist
