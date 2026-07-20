GOFLAGS      := -trimpath
DATE         := $(shell date +%y%m%d)
VERSION      := $(shell (git describe --tags --abbrev=0 2>/dev/null || echo "0.0.0") | sed 's/^v//')
COMMIT       := $(shell git rev-parse --short HEAD 2>/dev/null)
PLCC_API_URL := https://access.redhat.com/product-life-cycles/api/v2/products
LDFLAGS      += -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: build
build: plcc2fbc

.PHONY: plcc2fbc
plcc2fbc:
	go build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o bin/plcc2fbc ./cmd/plcc2fbc

.PHONY: test
test:
	go test -v -count 1 ./...

.PHONY: e2e
e2e:
	go test -v -count 1 ./test/e2e/

.PHONY: update-e2e
update-e2e: plcc2fbc
	bin/plcc2fbc -i test/e2e/testdata/plcc.json --validators none -o yaml test/e2e/testdata/reference-fbc.yaml
	bin/plcc2fbc -i test/e2e/testdata/plcc.json -o yaml test/e2e/testdata/reference-fbc-validated.yaml

.PHONY: update-e2e-source
update-e2e-source: plcc2fbc
	curl -sSf -o test/e2e/testdata/plcc.json $(PLCC_API_URL)
	$(MAKE) update-e2e

.PHONY: generate-fbc
generate-fbc: plcc2fbc
	mkdir -p fbc-samples
	bin/plcc2fbc -l fbc-samples/fbc-$(DATE).validation.jsonl fbc-samples/fbc-$(DATE).json >fbc-samples/fbc-$(DATE).log.jsonl
