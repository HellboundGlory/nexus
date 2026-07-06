.PHONY: build test lint run web web-dev web-test verify-web
build: web
	CGO_ENABLED=0 go build -o nexus ./cmd/nexus
test:
	go test ./...
run:
	go run ./cmd/nexus

web:
	cd web && npm ci && npm run build

web-dev:
	cd web && npm run dev

web-test:
	cd web && npm ci && npm test

verify-web: web
	git diff --exit-code web/dist || (echo "web/dist is stale — run 'make web' and commit"; exit 1)
