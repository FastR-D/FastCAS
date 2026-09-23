#!/usr/bin/env python3
"""Fail if a required real-provider contract was skipped or never executed."""

import json
import sys

REQUIRED = {
    "TestConsoleAgainstRealBrowser",
    "TestTypeScriptSDKAgainstProvider/node",
    "TestTypeScriptSDKAgainstProvider/bun",
    "TestPythonSDKAgainstProvider",
    "TestServiceExamplesAgainstProvider",
    "TestGoSDKLoginAndLinkContract",
    "TestGoSDKDeviceContract",
    "TestGoSDKIdentityStatusNotification",
    "TestAdminServiceAccountHTTPAndTokenLifecycle",
    "TestFastWriteAgainstProvider",
    "TestFastWriteAgainstRealBrowser",
    "TestFastWriteIdentityStatusAgainstProvider",
    "TestFastTaskAgainstProvider",
    "TestFastTaskAgainstRealBrowser",
    "TestFastTaskIdentityStatusAgainstProvider",
    "TestFastReadAgainstProvider",
    "TestFastReadAgainstRealBrowser",
    "TestFastReadIdentityStatusAgainstProvider",
    "TestFastResearchAgainstProvider",
    "TestFastResearchAgainstRealBrowser",
    "TestFastResearchIdentityStatusAgainstProvider",
    "TestFastNewsAgainstProvider",
    "TestFastNewsAgainstRealBrowser",
    "TestFastNewsIdentityStatusAgainstProvider",
    "TestFastNewsResearchDelegationAgainstProvider",
    "TestFastInsightAgainstProvider",
    "TestFastLabsAgainstProvider",
    "TestFastLabsAgainstRealBrowser",
}
CORE = {
    "TestTypeScriptSDKAgainstProvider/node",
    "TestTypeScriptSDKAgainstProvider/bun",
    "TestPythonSDKAgainstProvider",
    "TestServiceExamplesAgainstProvider",
    "TestGoSDKLoginAndLinkContract",
    "TestGoSDKDeviceContract",
    "TestGoSDKIdentityStatusNotification",
    "TestAdminServiceAccountHTTPAndTokenLifecycle",
    "TestOIDCHTTPLoginRefreshAndReplay",
    "TestRestrictedTokenExchange",
    "TestDeviceAuthorizationSingleUseAndBrowserConfirmation",
}
if sys.argv[1:] == ["--core"]:
    REQUIRED = CORE
elif sys.argv[1:]:
    raise SystemExit("usage: check_contract_results.py [--core]")

results = {}
failures = []
packages = set()
output = {}
for line in sys.stdin:
    try:
        item = json.loads(line)
    except json.JSONDecodeError:
        print(f"invalid go test JSON: {line[:200]}", file=sys.stderr)
        raise SystemExit(1)
    test = item.get("Test")
    action = item.get("Action")
    if test in REQUIRED and action == "output":
        output[test] = (output.get(test, "") + item.get("Output", ""))[-12000:]
    if test in REQUIRED and action in ("pass", "fail", "skip"):
        results[test] = action
        print(f"{action.upper():4} {test}", flush=True)
    if action == "fail":
        failures.append((item.get("Package", ""), test or "package"))
    if not test and action == "pass":
        packages.add(item.get("Package", ""))

missing = sorted(REQUIRED - results.keys())
not_passed = sorted(name for name, state in results.items() if state != "pass")
if missing or not_passed or failures or "github.com/FastR-D/FastCAS/internal/httpapi" not in packages:
    print(f"Contract gate failed: missing={missing}, not_passed={not_passed}, failures={failures}", file=sys.stderr)
    for name in not_passed:
        if name in output:
            print(f"--- {name} output ---\n{output[name]}", file=sys.stderr)
    raise SystemExit(1)
print(f"Contract gate passed: {len(REQUIRED)} required real-provider/browser cases", flush=True)
