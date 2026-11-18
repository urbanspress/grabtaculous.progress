#!/usr/bin/env python3
"""Package static CGO_ENABLED=0 release binaries, guide PDF, source bundle, and SHA256SUMS.txt."""

from __future__ import annotations

import argparse
import hashlib
import os
import shutil
import subprocess
import sys
import zipfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DIST_DIR = ROOT / "dist"

TARGETS = [
    ("windows", "amd64", "grabtaculous-windows-amd64.exe"),
    ("linux", "amd64", "grabtaculous-linux-amd64"),
    ("linux", "arm64", "grabtaculous-linux-arm64"),
]

BUNDLE_DIRS = [
    "cmd",
    "internal",
    "catalog",
    "tests",
    "specifications",
    "docs",
    "scripts",
]

BUNDLE_FILES = [
    "go.mod",
    "AGENTS.md",
    "GRABTACULOUS-EXPERT.md",
    "README.md",
    "CHANGELOG.md",
    "HANDOFF.md",
    "LICENSE",
    ".gitignore",
    "vps-starter-kit.md",
]


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            h.update(chunk)
    return h.hexdigest()


def build_binaries(version: str) -> list[Path]:
    built: list[Path] = []
    ldflags = f"-s -w -X main.Version={version}"
    for goos, goarch, out_name in TARGETS:
        out_path = DIST_DIR / out_name
        env = os.environ.copy()
        env["CGO_ENABLED"] = "0"
        env["GOOS"] = goos
        env["GOARCH"] = goarch
        cmd = ["go", "build", "-trimpath", "-ldflags", ldflags, "-o", str(out_path), "./cmd/grabtaculous"]
        print(f"Building {out_name} ({goos}/{goarch})...")
        res = subprocess.run(cmd, cwd=ROOT, env=env, capture_output=True, text=True, check=False)
        if res.returncode != 0 or not out_path.is_file():
            print(f"Build failed for {out_name}: {res.stderr}\n{res.stdout}", file=sys.stderr)
            raise SystemExit(1)
        built.append(out_path)
    return built


def collect_bundle_paths() -> list[Path]:
    paths: list[Path] = []
    for rel_file in BUNDLE_FILES:
        p = ROOT / rel_file
        if p.is_file():
            paths.append(p)
    for rel_dir in BUNDLE_DIRS:
        base = ROOT / rel_dir
        if not base.exists():
            continue
        for p in base.rglob("*"):
            if not p.is_file():
                continue
            if "__pycache__" in p.parts or p.suffix == ".pyc":
                continue
            paths.append(p)
    paths.sort(key=lambda p: p.relative_to(ROOT).as_posix())
    return paths


def create_bundle(version: str) -> Path:
    bundle_path = DIST_DIR / f"grabtaculous-v{version}-bundle.zip"
    prefix = f"grabtaculous-v{version}"
    files = collect_bundle_paths()
    with zipfile.ZipFile(bundle_path, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as zf:
        for src in files:
            rel = src.relative_to(ROOT).as_posix()
            arcname = f"{prefix}/{rel}"
            zi = zipfile.ZipInfo(arcname, date_time=(2026, 11, 15, 0, 0, 0))
            zi.create_system = 3
            mode = 0o100755 if src.suffix in (".sh", ".py") else 0o100644
            zi.external_attr = mode << 16
            zf.writestr(zi, src.read_bytes())
    return bundle_path


def write_checksums(files: list[Path]) -> Path:
    sums_path = DIST_DIR / "SHA256SUMS.txt"
    lines = []
    for p in sorted(files, key=lambda x: x.name):
        lines.append(f"{sha256_file(p)}  {p.name}")
    sums_path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return sums_path


def main() -> int:
    parser = argparse.ArgumentParser(description="Package Grabtaculous release artifacts into dist/.")
    parser.add_argument("version", help="Release version without leading 'v', e.g. 0.1.0")
    args = parser.parse_args()
    version = args.version.lstrip("v").strip()

    if DIST_DIR.exists():
        shutil.rmtree(DIST_DIR)
    DIST_DIR.mkdir(parents=True, exist_ok=True)

    guide_pdf = ROOT / "docs" / "releases" / "pdf" / f"Grabtaculous-v{version}-Guide.pdf"
    if not guide_pdf.is_file() or guide_pdf.stat().st_size == 0:
        print(f"Error: Missing release guide PDF at {guide_pdf}. Run generate_release_guides.py first.", file=sys.stderr)
        return 1

    artifacts: list[Path] = []
    artifacts.extend(build_binaries(version))

    dist_pdf = DIST_DIR / guide_pdf.name
    shutil.copy2(guide_pdf, dist_pdf)
    artifacts.append(dist_pdf)

    artifacts.append(create_bundle(version))
    write_checksums(artifacts)

    print("\nPackaged release artifacts:")
    for item in sorted(DIST_DIR.iterdir(), key=lambda x: x.name):
        print(f"  - {item.name} ({item.stat().st_size} bytes)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
