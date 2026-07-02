.PHONY: build test lint run
build:
	CGO_ENABLED=0 go build -o nexus ./cmd/nexus
test:
	go test ./...
run:
	go run ./cmd/nexus
