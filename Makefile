.PHONY: tidy fmt vet test lint check build-examples

tidy:
	go mod tidy

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test ./...

lint:
	golangci-lint run ./...

build-examples:
	go build -o /dev/null ./examples/basic/

check: tidy vet test build-examples
	@command -v golangci-lint >/dev/null 2>&1 && $(MAKE) lint || echo "golangci-lint not installed; skipping lint"
