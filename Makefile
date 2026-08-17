GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
EXE := server/plugin-$(GOOS)-$(GOARCH)
ifeq ($(GOOS),windows)
EXE := $(EXE).exe
endif

.PHONY: build test vet fmt clean
build:
	go build -o $(EXE) ./cmd/kandev-plugin-youtrack

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -f server/plugin-*