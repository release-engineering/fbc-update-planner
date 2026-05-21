GOFLAGS := -trimpath
DATE    := $(shell date +%y%m%d)

.PHONY: build
build: plcc2fbc

.PHONY: plcc2fbc
plcc2fbc:
	go build $(GOFLAGS) -o bin/plcc2fbc ./cmd/plcc2fbc

.PHONY: test
test:
	go test -v ./...

.PHONY: generate-fbc
generate-fbc: plcc2fbc
	bin/plcc2fbc -w fbc-samples/fbc-$(DATE).yaml 2>fbc-samples/fbc-$(DATE).log
	cp -f fbc-samples/fbc-$(DATE).yaml fbc-samples/fbc-latest.yaml
	cp -f fbc-samples/fbc-$(DATE).log fbc-samples/fbc-latest.log
