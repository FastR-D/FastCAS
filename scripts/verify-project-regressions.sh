#!/usr/bin/env bash
set -euo pipefail

cas_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workspace="$(dirname "$cas_root")"

# Keep the projects' own local-account and FastCAS regressions beside the
# provider contracts. These checks catch breakage that a successful callback
# or signed-event roundtrip alone cannot see.
(cd "$workspace/FastWrite" && bun test apps/server/src/auth/identity-provider.test.ts apps/server/src/auth/fastcas-service.test.ts)
(cd "$workspace/FastTask" && go test -race ./internal/httpapi ./internal/platform/auth ./internal/persistence)
(cd "$workspace/FastRead" && PYTHONPATH=backend:../FastCAS/sdk/python/src .venv/bin/python -m pytest -q \
  backend/tests/test_local_accounts.py backend/tests/test_fastcas_store.py backend/tests/test_fastcas_service.py)
(cd "$workspace/FastResearch" && node --test server/account-migration.test.mjs server/account-store.test.mjs \
  server/runtime.test.mjs server/fastcas-store.test.mjs server/fastcas-service.test.mjs server/service-ingest.test.mjs)
(cd "$workspace/FastNews" && PYTHONPATH=.:../FastCAS/sdk/python/src .venv/bin/python -m pytest -q \
  tests/test_accounts.py tests/test_accounts_http.py tests/test_cas_store.py tests/test_cas_service.py tests/test_research_import.py)
(cd "$workspace/FastInsight" && PYTHONPATH=.:scripts:../FastCAS/sdk/python/src python3 -m unittest discover -s tests -p 'test_*.py' -v)
(cd "$workspace/FastLabs" && PYTHONPATH=.:tests python3 -m unittest discover -s tests -p 'test_*.py')
