#!/usr/bin/env bash
set -euo pipefail

cas_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$cas_root"

for command_name in go node bun npm python3 corepack; do
  command -v "$command_name" >/dev/null || { echo "Missing required command: $command_name" >&2; exit 2; }
done
[[ -s .local/test-database-url || -n "${FASTCAS_TEST_DATABASE_URL:-}" ]] || { echo "Set FASTCAS_TEST_DATABASE_URL or .local/test-database-url" >&2; exit 2; }
[[ -x /usr/bin/google-chrome ]] || { echo "Chrome is required for the browser contract" >&2; exit 2; }
[[ -x .venv/bin/python ]] || { echo "Install the Python SDK in FastCAS/.venv" >&2; exit 2; }
[[ -x ../FastRead/.venv/bin/python ]] || { echo "Install FastRead's FastAPI test dependencies" >&2; exit 2; }
[[ -x ../FastNews/.venv/bin/python ]] || { echo "Install FastNews's account test dependencies" >&2; exit 2; }
for sibling in FastWrite FastTask FastRead FastResearch FastNews FastInsight FastLabs; do
  [[ -d "../$sibling" ]] || { echo "Missing project checkout: $sibling" >&2; exit 2; }
done

python3 api/check.py
(cd sdk/typescript && npm run build)
(cd web && npm run build)
(cd ../FastWrite && bun run --filter @fastwrite/web build)
(cd ../FastResearch && npm run build)
(cd ../FastTask/web && npm run build)
(cd ../FastRead/fastread-frontend && corepack pnpm run typecheck)
(cd ../FastRead/fastread-frontend && corepack pnpm run build)
(cd sdk/go && go test -race ./...)
.venv/bin/python -m unittest discover -s sdk/python/tests -p 'test_*.py'
PYTHONPATH="$cas_root/sdk/python/src" ../FastRead/.venv/bin/python -m unittest sdk/python/tests/test_fastapi.py
bash scripts/verify-project-regressions.sh

export FASTCAS_SDK_CONTRACT=1 FASTCAS_PROJECT_CONTRACT=1 FASTCAS_BROWSER_CONTRACT=1
go test -json -race -count=1 ./... | python3 scripts/check_contract_results.py
