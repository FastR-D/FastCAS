.PHONY: test build vet api-check release verify-project-regressions verify-contracts verify-capacity
test: api-check
	go test -race ./...
api-check:
	python3 api/check.py
vet:
	go vet ./...
build:
	go build -trimpath -o bin/fastcas ./cmd/fastcas
release:
	python3 scripts/build-release.py
verify-contracts:
	bash scripts/verify-contracts.sh
verify-project-regressions:
	bash scripts/verify-project-regressions.sh
verify-capacity:
	@test -s .local/test-database-url || test -n "$$FASTCAS_TEST_DATABASE_URL" || { echo 'Set FASTCAS_TEST_DATABASE_URL or .local/test-database-url' >&2; exit 2; }
	FASTCAS_LOAD_TEST=1 go test -race -count=1 -parallel 8 -run '^TestCapacity(LoginAndRefreshBurst|OutboxBacklog|OutboxRecovery)$$' -v ./internal/httpapi ./internal/core
