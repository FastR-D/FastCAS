#!/usr/bin/env python3
"""Build, smoke-test and optionally publish the exact SDK archives tested."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parent.parent
NPM = "https://registry.npmjs.org/"
PYPI = "https://upload.pypi.org/legacy/"


def run(*args, cwd=ROOT, env=None):
    result = subprocess.run(args, cwd=cwd, env=env, text=True, stdout=subprocess.PIPE)
    if result.returncode:
        print(result.stdout, file=sys.stderr)
        result.check_returncode()
    return result.stdout


def version():
    value = json.loads((ROOT / "sdk/typescript/package.json").read_text())["version"]
    match = re.search(r'^version\s*=\s*"([^"]+)"', (ROOT / "sdk/python/pyproject.toml").read_text(), re.M)
    if not match or match[1] != value:
        raise SystemExit("TypeScript and Python SDK versions differ")
    if not re.fullmatch(r"\d+\.\d+\.\d+", value):
        raise SystemExit("SDK publishing currently supports stable X.Y.Z versions only")
    tag = os.environ.get("RELEASE_TAG")
    if tag and tag != f"v{value}":
        raise SystemExit(f"Release tag {tag} does not match SDK version v{value}")
    print(f"SDK version {value}; Go tag sdk/go/v{value}", flush=True)
    return value


def confirm(name, value, registry):
    print(f"Publish {name} {value} to {registry}", flush=True)
    if os.environ.get("FASTCAS_PUBLISH_CONFIRM") != "1":
        if input("Continue? [y/N] ") != "y":
            raise SystemExit("Publishing cancelled")


def python_package(stage, value, publish):
    output = stage / "python"
    run("uv", "build", "--python", sys.executable, "--wheel", "--out-dir", str(output), str(ROOT / "sdk/python"))
    wheels = list(output.glob(f"fastcas_sdk-{value}-*.whl"))
    if len(wheels) != 1:
        raise SystemExit("Expected exactly one wheel")
    wheel = wheels[0]
    with zipfile.ZipFile(wheel) as archive:
        assert "fastcas/py.typed" in archive.namelist(), "wheel missing py.typed"
        assert any(n.endswith("/LICENSE") for n in archive.namelist()), "wheel missing LICENSE"
    venv = stage / "venv"
    run("uv", "venv", "--python", sys.executable, str(venv))
    python = venv / "bin/python"
    run("uv", "pip", "install", "--python", str(python), str(wheel))
    run(str(python), "-I", "-c", "from fastcas import FastCAS, AsyncFastCAS; assert FastCAS and AsyncFastCAS", cwd=stage)
    if publish:
        confirm("fastcas-sdk", value, PYPI)
        run("uv", "publish", "--publish-url", PYPI, "--trusted-publishing", "automatic", str(wheel))
    print(f"Verified Python wheel: {wheel.name}", flush=True)


def npm_package(stage, value, publish):
    package = ROOT / "sdk/typescript"
    run("pnpm", "install", "--frozen-lockfile", "--ignore-scripts", cwd=package)
    run("pnpm", "run", "build", cwd=package)
    packed = json.loads(run("pnpm", "pack", "--pack-destination", str(stage), "--json", cwd=package))
    tarball = Path(packed["filename"])
    if not tarball.is_absolute():
        tarball = stage / tarball
    import tarfile
    with tarfile.open(tarball) as archive:
        assert "package/LICENSE" in archive.getnames(), "tarball missing LICENSE"
    smoke = stage / "node"
    smoke.mkdir()
    (smoke / "package.json").write_text('{"private":true,"type":"module"}\n')
    run("pnpm", "add", "--ignore-scripts", str(tarball), cwd=smoke)
    run("node", "--input-type=module", "-e",
        "import {FastCAS} from '@fastrd/fastcas'; import {FastCAS as Server} from '@fastrd/fastcas/server'; import {loginURL} from '@fastrd/fastcas/browser'; if(FastCAS!==Server || typeof Server!=='function'||!loginURL().startsWith('/')) process.exit(1)", cwd=smoke)
    if publish:
        confirm("@fastrd/fastcas", value, NPM)
        env = os.environ.copy()
        # NODE_AUTH_TOKEN alone is not read by npm; configure its registry mapping.
        # Keep existing user configuration (pnpm login) when no token is supplied.
        if env.get("NODE_AUTH_TOKEN"):
            config = stage / "publish.npmrc"
            config.write_text('//registry.npmjs.org/:_authToken=${NODE_AUTH_TOKEN}\n')
            config.chmod(0o600)
            env["NPM_CONFIG_USERCONFIG"] = str(config)
        args = ["pnpm", "publish", str(tarball), "--access", "public", "--registry", NPM, "--no-git-checks"]
        if env.get("GITHUB_ACTIONS") == "true":
            args.append("--provenance")
        run(*args, cwd=stage, env=env)
    print(f"Verified npm tarball: {tarball.name}", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("target", choices=["check", "dry-run", "pypi-dry-run", "npm-dry-run", "pypi", "npm"])
    args = parser.parse_args()
    value = version()
    if args.target == "check":
        return
    with tempfile.TemporaryDirectory(prefix="fastcas-publish-") as directory:
        stage = Path(directory)
        if args.target in ("dry-run", "pypi-dry-run", "pypi"):
            python_package(stage, value, args.target == "pypi")
        if args.target in ("dry-run", "npm-dry-run", "npm"):
            npm_package(stage, value, args.target == "npm")


if __name__ == "__main__":
    main()
