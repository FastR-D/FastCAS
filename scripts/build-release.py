#!/usr/bin/env python3
"""Build and smoke-test a local FastCAS release bundle without publishing it."""

import argparse
import gzip
import hashlib
import json
import os
import shutil
import subprocess
import tarfile
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def run(*args: str, cwd: Path = ROOT, env: dict[str, str] | None = None) -> str:
    result = subprocess.run(args, cwd=cwd, env=env, text=True, capture_output=True)
    if result.returncode:
        raise RuntimeError(f"{' '.join(args)} failed ({result.returncode}):\n{result.stderr[-4000:]}")
    return result.stdout


def archive(source: Path, target: Path, prefix: str, files: list[Path]) -> None:
    with target.open("wb") as raw, gzip.GzipFile(filename="", fileobj=raw, mode="wb", mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode="w") as output:
            for path in sorted(files):
                if not path.is_file():
                    continue
                item = tarfile.TarInfo(f"{prefix}/{path.relative_to(source).as_posix()}")
                item.size = path.stat().st_size
                item.mode = 0o644
                item.mtime = item.uid = item.gid = 0
                with path.open("rb") as content:
                    output.addfile(item, content)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, help="empty output directory (default: dist/release-vVERSION)")
    args = parser.parse_args()
    ts = ROOT / "sdk/typescript"
    py = ROOT / "sdk/python"
    go = ROOT / "sdk/go"
    version = json.loads((ts / "package.json").read_text())["version"]
    pyproject = (py / "pyproject.toml").read_text()
    if f'version = "{version}"' not in pyproject:
        raise SystemExit("TypeScript and Python SDK versions differ")
    output = (args.output or ROOT / "dist" / f"release-v{version}").resolve()
    if output.exists() and any(output.iterdir()):
        raise SystemExit(f"output directory must be empty: {output}")

    with tempfile.TemporaryDirectory(prefix="fastcas-release-") as temporary:
        stage = Path(temporary)
        run("python3", "api/check.py")
        run("npm", "run", "build", cwd=ts)
        run("npm", "run", "build", cwd=ROOT / "web")
        npm_result = json.loads(run("npm", "pack", "--pack-destination", str(stage), "--json", cwd=ts))
        npm_package = stage / npm_result[0]["filename"]
        run("python3", "-m", "pip", "wheel", "--no-deps", "--no-build-isolation", "--wheel-dir", str(stage), str(py),
            env={**os.environ, "SOURCE_DATE_EPOCH": "315532800"})
        wheels = list(stage.glob(f"fastcas_sdk-{version}-*.whl"))
        if len(wheels) != 1:
            raise RuntimeError("expected exactly one Python SDK wheel")
        wheel = wheels[0]
        build_env = {**os.environ, "CGO_ENABLED": "0", "GOOS": "linux", "GOARCH": "amd64"}
        binary = stage / f"fastcas-{version}-linux-amd64"
        run("go", "build", "-trimpath", "-o", str(binary), "./cmd/fastcas", env=build_env)
        binary.chmod(0o755)
        web = ROOT / "web/dist"
        web_bundle = stage / f"fastcas-web-{version}.tar.gz"
        archive(web, web_bundle, "web", list(web.rglob("*")))
        go_bundle = stage / f"fastcas-go-sdk-{version}.tar.gz"
        go_files = [go / "README.md", go / "go.mod", go / "go.sum", *go.rglob("*.go")]
        archive(go, go_bundle, "sdk-go", go_files)
        shutil.copy2(ROOT / "api/openapi.json", stage / f"fastcas-openapi-{version}.json")

        # Install archives in isolated directories, so source-tree imports cannot mask broken packages.
        smoke = stage / "smoke"
        smoke.mkdir()
        run("npm", "install", "--ignore-scripts", "--no-audit", "--no-fund", "--prefix", str(smoke), str(npm_package), cwd=smoke)
        run("node", "--input-type=module", "-e",
            "import {FastCAS} from '@fastrd/fastcas/server'; import {loginURL} from '@fastrd/fastcas/browser'; if(typeof FastCAS!=='function'||!loginURL().startsWith('/')) process.exit(1)", cwd=smoke)
        python_site = stage / "python-site"
        run("python3", "-m", "pip", "--disable-pip-version-check", "--quiet", "install", "--target", str(python_site), str(wheel))
        run("python3", "-S", "-c", "import sys; sys.path.insert(0, sys.argv[1]); from fastcas import FastCAS, AsyncFastCAS; assert FastCAS and AsyncFastCAS", str(python_site), cwd=smoke)
        go_extract = stage / "go-consumer"
        go_extract.mkdir()
        with tarfile.open(go_bundle, "r:gz") as source:
            source.extractall(go_extract, filter="data")
        run("go", "test", "./...", cwd=go_extract / "sdk-go", env={**os.environ, "GOWORK": "off"})

        artifacts = [npm_package, wheel, binary, web_bundle, go_bundle, stage / f"fastcas-openapi-{version}.json"]
        manifest = {"version": version, "target": "linux-amd64", "artifacts": [path.name for path in artifacts],
                    "notes": "Local build only; Go module still requires a repository tag and SDKs are not published."}
        (stage / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
        artifacts.append(stage / "manifest.json")
        checksums = "".join(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n" for path in sorted(artifacts))
        (stage / "SHA256SUMS").write_text(checksums)
        output.mkdir(parents=True, exist_ok=True)
        for path in [*artifacts, stage / "SHA256SUMS"]:
            shutil.copy2(path, output / path.name)
    print(f"Verified local release bundle: {output}")


if __name__ == "__main__":
    main()
