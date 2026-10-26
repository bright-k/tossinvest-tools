.PHONY: bootstrap build generate check-generated test race vet check mcp-check codex-check cli-check audit-secrets verify
bootstrap:
	./scripts/bootstrap
build:
	mkdir -p bin
	./scripts/go build -trimpath -o bin/tossinvest-tools ./cmd/tossinvest-tools
	shasum -a 256 bin/tossinvest-tools > bin/SHA256SUMS

generate:
	./scripts/generate
check-generated:
	./scripts/check-generated

test:
	./scripts/go test ./...
race:
	./scripts/go test -race ./...
vet:
	./scripts/go vet ./...
check:
	python3 scripts/check-specs.py
mcp-check: build
	./scripts/go run ./cmd/mcp-check ./bin/tossinvest-tools
codex-check: build
	python3 scripts/check-codex.py
cli-check: build
	python3 scripts/check-cli.py
audit-secrets:
	python3 scripts/audit-secrets.py
verify: check check-generated test race vet mcp-check
