GOFLAGS := -trimpath
DATE    := $(shell date +%y%m%d)

.PHONY: build
build: plcc2fbc

.PHONY: plcc2fbc
plcc2fbc:
	go build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o bin/plcc2fbc ./cmd/plcc2fbc

.PHONY: test
test:
	go test -v -count 1 ./...

.PHONY: generate-fbc
generate-fbc: plcc2fbc
	mkdir -p fbc-samples
	bin/plcc2fbc -l fbc-samples/fbc-$(DATE).validation.jsonl fbc-samples/fbc-$(DATE).json >fbc-samples/fbc-$(DATE).log.jsonl
