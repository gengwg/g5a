VERSION=$(shell git describe --tags --dirty --always)
PYTHON ?= python3

.PHONY: build
build:
	go build -ldflags "-X 'main.version=${VERSION}'" -o g5a ./cmd/g5a

.PHONY: test
test:
	go test -v ./...

.PHONY: test-race
test-race:
	go test -race ./...

.PHONY: test-integration
test-integration:
	go test -v -tags integration -count=1 -timeout 5m ./...

.PHONY: test-terminal
test-terminal:
	@set -e; \
	work_dir=$$(mktemp -d "$${TMPDIR:-/tmp}/g5a-terminal-build.XXXXXX"); \
	trap 'rm -rf "$$work_dir"' EXIT; \
	go build -o "$$work_dir/g5a" ./cmd/g5a; \
	for suite in e2e_g5a.py e2e_row_repeats.py e2e_v2_layouts.py; do \
		G5A_BINARY="$$work_dir/g5a" $(PYTHON) "e2e/$$suite"; \
	done

.PHONY: lint
lint:
	golangci-lint run

.PHONY: release
release:
	./scripts/build-release.sh $(VERSION)

.PHONY: clean
clean:
	rm -f g5a
