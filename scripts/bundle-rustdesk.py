#!/usr/bin/env python3
"""Bundle the RustDesk core from its pinned upstream desktop release."""

import argparse
import hashlib
from http.client import HTTPException
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
from urllib.request import Request, urlopen


# This release must match coreVersion in internal/rustdesk/bridge.go.
VERSION = "1.5.3"
REPOSITORY = "https://github.com/rustdesk/rustdesk"
COMMIT = "95cca3f661f635b3cc85c04935fe8d96d8f432ef"
CACHE = Path(__file__).resolve().parents[1] / ".native/rustdesk/releases"
ASSETS = {
    "linux-amd64": ("x86_64.deb", 727039144, "81096d2f65fd43352540c351bca15bb0712a2a6f9efa4c95e9fff25cb97b3b81"),
    "linux-arm64": ("aarch64.deb", 727063822, "fa85e8d3ccd45e2e303c2d1359af5dc51aa93d880e39ba47c9b04bfabd82ae19"),
    "macos-amd64": ("x86_64.dmg", 727209115, "74d06e6b2e273a128e8d36313e30d47db2063f453a4235ed4d9e5cdfdf9be538"),
    "macos-arm64": ("aarch64.dmg", 727209114, "e6d30fa1633d772c48935907c208ca4e555a856fb2038af3aaa87a327c29360b"),
    "windows-amd64": ("x86_64.msi", 727209108, "27f6bfb48a93d524860dd7f4a79e349be14b6dc71295746cf132a060ba4869c8"),
    "windows-arm64": ("aarch64.msi", 727209102, "b41893aed2cf411d557babc3cd02b74836dd199fc152a3b63147591f52f981aa"),
}
LICENSE_SHA256 = "8486a10c4393cee1c25392769ddd3b2d6c242d6ec7928e1414efff7dfb2f07ef"
# The File table of both pinned MSIs maps this cabinet entry to librustdesk.dll.
MSI_CORE_ENTRY = "filjWrhjy7FT9YbbOZFpnXRqG2pYpQ"


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as file:
        for block in iter(lambda: file.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def download(filename, checksum, urls):
    CACHE.mkdir(parents=True, exist_ok=True)
    cached = CACHE / filename
    if cached.is_file() and sha256(cached) == checksum:
        return cached
    errors = []
    with tempfile.TemporaryDirectory(dir=CACHE) as work:
        pending = Path(work) / filename
        for url in urls:
            try:
                request = Request(url, headers={"Accept": "application/octet-stream"})
                with urlopen(request, timeout=30) as response, pending.open("wb") as output:
                    shutil.copyfileobj(response, output)
                if sha256(pending) != checksum:
                    raise ValueError("SHA-256 mismatch")
            except (OSError, HTTPException, ValueError) as error:
                errors.append(f"{url}: {error}")
                continue
            pending.replace(cached)
            return cached
    raise RuntimeError(f"Could not retrieve {filename}: " + " | ".join(errors))


def extract_7zip(archive, work, entry):
    tool = next((path for name in ("7zz", "7z", "7z.exe") if (path := shutil.which(name))), None)
    if tool is None:
        raise RuntimeError("7-Zip is required to extract the RustDesk release")
    subprocess.run([tool, "e", "-y", "-bso0", f"-o{work}", str(archive), entry], check=True)


def extract_core(archive, work, output):
    if archive.suffix == ".deb":
        subprocess.run(["dpkg-deb", "-x", str(archive), str(work)], check=True)
        source = work / "usr/share/rustdesk/lib/librustdesk.so"
    elif archive.suffix == ".msi":
        extract_7zip(archive, work, MSI_CORE_ENTRY)
        source = work / MSI_CORE_ENTRY
    elif sys.platform == "darwin":
        mount = work / "mounted"
        mount.mkdir()
        subprocess.run(["hdiutil", "attach", "-nobrowse", "-readonly", "-mountpoint", str(mount), str(archive)], check=True)
        try:
            shutil.copyfile(mount / "RustDesk.app/Contents/Frameworks/liblibrustdesk.dylib", output)
        finally:
            subprocess.run(["hdiutil", "detach", str(mount)], check=True)
        return
    else:
        extract_7zip(archive, work, "-ir!liblibrustdesk.dylib")
        source = work / "liblibrustdesk.dylib"
    shutil.copyfile(source, output)


def bundle(target, directory):
    if not directory.is_dir():
        raise ValueError(f"Package directory does not exist: {directory}")
    suffix, asset_id, checksum = ASSETS[target]
    filename = f"rustdesk-{VERSION}-{suffix}"
    asset_url = f"{REPOSITORY}/releases/download/{VERSION}/{filename}"
    archive = download(filename, checksum, [
        f"https://api.github.com/repos/rustdesk/rustdesk/releases/assets/{asset_id}?download=1",
        asset_url,
    ])
    license_file = download("LICENCE", LICENSE_SHA256, [
        f"https://raw.githubusercontent.com/rustdesk/rustdesk/{COMMIT}/LICENCE",
    ])
    system, _ = target.split("-")
    library, notice_directory = {
        "linux": ("lib/librustdesk.so", "NativeLicenses/rustdesk"),
        "macos": ("Contents/Frameworks/liblibrustdesk.dylib", "Contents/Resources/NativeLicenses/rustdesk"),
        "windows": ("librustdesk.dll", "NativeLicenses/rustdesk"),
    }[system]
    output = directory / library
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=output.parent) as work:
        binary = Path(work) / "core"
        extract_core(archive, Path(work), binary)
        binary.chmod(0o755)
        binary.replace(output)
    notices = directory / notice_directory
    notices.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(license_file, notices / "LICENCE")
    metadata = {
        "version": VERSION,
        "license": "AGPL-3.0",
        "source": f"{REPOSITORY}/tree/{COMMIT}",
        "git_repository": REPOSITORY + ".git",
        "build_recipe": f"{REPOSITORY}/blob/{COMMIT}/.github/workflows/flutter-build.yml",
        "target": target,
        "upstream_archive": asset_url,
        "upstream_archive_sha256": checksum,
    }
    (notices / "manifest.json").write_text(json.dumps(metadata, indent=2) + "\n", encoding="utf-8")
    print(f"Bundled RustDesk {VERSION} in {output}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("target", choices=ASSETS)
    parser.add_argument("directory", type=Path, help="staged package directory (the .app on macOS)")
    args = parser.parse_args()
    bundle(args.target, args.directory.resolve())
