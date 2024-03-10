.DEFAULT_GOAL = build

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: lint
lint:
	golangci-lint run --timeout 5m

.PHONY: fix
fix:
	golangci-lint run --fix

.PHONY: build
build: fmt fix
	go build -o dist/bcdl
