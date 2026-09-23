.PHONY: fmt lint test test-race bench build tidy check examples

fmt:
	gofmt -s -w .

lint:
	golangci-lint run

test:
	go test ./... -count=1

test-race:
	go test ./... -race -count=1

bench:
	go test ./z_test -bench . -benchmem

build:
	go build ./...

tidy:
	go mod tidy

check: fmt tidy lint test-race

examples:
	for d in ./examples/*; do if [ -d "$$d" ]; then (cd "$$d" && go run . || true); fi; done
