VERSION=$(shell git describe --tags --dirty --always)

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

.PHONY: lint
lint:
	golangci-lint run

.PHONY: release
release:
	./scripts/build-release.sh $(VERSION)

.PHONY: clean
clean:
	rm -f g5a
