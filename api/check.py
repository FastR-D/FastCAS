#!/usr/bin/env python3
"""Check the OpenAPI artifact against the actual binding/admin route table."""

import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
API = Path(__file__).resolve().parent
document = json.loads((API / "openapi.json").read_text(encoding="utf-8"))
assert document["openapi"].startswith("3.1.")

route = re.compile(r'r\.(Get|Post|Put|Patch|Delete)\("(/api/v1/[^\"]*)"')
actual = set()
for source in (ROOT / "internal/httpapi/server.go", ROOT / "internal/httpapi/accounts.go"):
    actual.update((path, method.lower()) for method, path in route.findall(source.read_text(encoding="utf-8")))
documented = {(path, method) for path, methods in document["paths"].items() for method in methods}
if actual != documented:
    raise SystemExit(f"route mismatch: undocumented={sorted(actual - documented)}, nonexistent={sorted(documented - actual)}")

schemes = document["components"]["securitySchemes"]
schemas = document["components"]["schemas"]
operation_ids = set()


def check_refs(value):
    if isinstance(value, dict):
        for key, item in value.items():
            if key == "$ref":
                prefix = "#/components/schemas/"
                if not isinstance(item, str) or not item.startswith(prefix) or item[len(prefix):] not in schemas:
                    raise SystemExit(f"unknown schema reference: {item}")
            else:
                check_refs(item)
    elif isinstance(value, list):
        for item in value:
            check_refs(item)


check_refs(document)
for path, methods in document["paths"].items():
    for method, operation in methods.items():
        ident = operation["operationId"]
        if ident in operation_ids:
            raise SystemExit(f"duplicate operationId: {ident}")
        operation_ids.add(ident)
        auth = operation["security"]
        if path.startswith("/api/v1/admin/"):
            expected = "adminSession"
        elif path.startswith(("/api/v1/link-intents", "/api/v1/account-links")):
            expected = "clientBasic"
        elif path in ("/api/v1/register", "/api/v1/recover", "/api/v1/recover-mfa"):
            expected = "public"
        elif path == "/api/v1/device-installations":
            expected = "deviceAccess"
        elif path.startswith("/api/v1/device-installations/"):
            expected = "installationSecret"
        else:
            expected = "browserSession"
        if auth != ([] if expected == "public" else [{expected: []}]) or (expected != "public" and expected not in schemes):
            raise SystemExit(f"incorrect auth for {method.upper()} {path}")
        if not any(200 <= int(code) < 300 for code in operation["responses"]):
            raise SystemExit(f"success response missing: {method.upper()} {path}")
        if expected in ("adminSession", "browserSession") and method == "post":
            parameters = {(item["name"], item["in"]) for item in operation.get("parameters", [])}
            if not {("Origin", "header"), ("X-CSRF-Token", "header")} <= parameters:
                raise SystemExit(f"CSRF/origin contract missing: {method.upper()} {path}")
        if expected == "public" and method == "post":
            parameters = {(item["name"], item["in"]) for item in operation.get("parameters", [])}
            if ("Origin", "header") not in parameters:
                raise SystemExit(f"origin contract missing: {method.upper()} {path}")
        for name in re.findall(r"\{([^}]+)\}", path):
            if not any(item["name"] == name and item["in"] == "path" and item["required"] for item in operation.get("parameters", [])):
                raise SystemExit(f"path parameter {name} missing: {method.upper()} {path}")

generated = subprocess.run(["python3", str(API / "generate.py"), "--check"], cwd=ROOT, check=True, capture_output=True)
if generated.stdout or generated.stderr:
    raise SystemExit("generator wrote unexpected output")
print(f"OpenAPI contract checked: {len(documented)} implemented JSON operations")
