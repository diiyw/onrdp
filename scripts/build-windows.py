#!/usr/bin/env python3
"""Build a portable OnRDP Windows release with its native libraries."""

import hashlib
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
from zipfile import ZipFile


ROOT = Path(__file__).resolve().parents[1]
# Windows 10/11 system DLLs omitted by older MinGW import libraries.
WINDOWS_SYSTEM_DLLS = {"bcryptprimitives.dll", "sas.dll"}


def output(*args):
    return subprocess.check_output(args, cwd=ROOT, text=True).strip()


def tool(name, prefix):
    path = shutil.which(prefix + name)
    if path is None and name == "objdump":
        path = shutil.which("llvm-objdump")
    if path is None:
        raise RuntimeError(f"{prefix}{name} is required to package the Windows build")
    return path


def bundle_runtime(app, arch, host):
    prefix = "" if host == "windows" else {"amd64": "x86_64", "arm64": "aarch64"}[arch] + "-w64-mingw32-"
    objdump = tool("objdump", prefix)
    compiler = tool("gcc", prefix)
    pkg_config = output("go", "env", "PKG_CONFIG")
    runtime = os.environ.get("FREERDP_RUNTIME_DIR")
    if not runtime:
        runtime = str(Path(output(pkg_config, "--variable=prefix", "freerdp3")) / "bin")
    available = {path.name.lower(): path for path in Path(runtime).glob("*.dll")}
    pending = [app / "onrdp.exe", app / "librustdesk.dll", app / "tools/scrcpy/libonrdp-scrcpy.dll"]
    seen = {path.name.lower() for path in pending}

    while pending:
        binary = pending.pop()
        for name in re.findall(r"DLL Name:\s+([^\s]+)", output(objdump, "-p", str(binary))):
            lower = name.lower()
            if lower in seen:
                continue
            seen.add(lower)
            if lower.startswith(("api-ms-win-", "ext-ms-win-")):
                continue
            source = available.get(lower)
            if source is not None:
                packaged = app / source.name
                shutil.copy2(source, packaged)
                pending.append(packaged)
                continue
            if host == "windows":
                system_dll = Path(os.environ["SystemRoot"]) / "System32" / name
                if system_dll.is_file():
                    continue
            else:
                import_lib = f"lib{Path(lower).stem}.a"
                if lower in WINDOWS_SYSTEM_DLLS or output(compiler, "-print-file-name=" + import_lib) != import_lib:
                    continue
            raise RuntimeError(f"{binary.name} requires {name}; supply it in FREERDP_RUNTIME_DIR")


def build():
    host, arch = output("go", "env", "GOHOSTOS", "GOHOSTARCH").splitlines()
    scrcpy_target = {"amd64": "win64", "arm64": "winarm64"}.get(arch)
    if scrcpy_target is None or host not in ("windows", "linux"):
        raise RuntimeError("Windows packages require a Windows or Linux amd64/arm64 build host")
    subprocess.run([
        "go", "run", "github.com/onix-engine/onix/editor/cmd/onix@v0.1.10",
        "build", "-platform", "windows", "-mode", "release", "-package", "portable", str(ROOT),
    ], cwd=ROOT, check=True)
    dist = ROOT / "dist/windows"
    archive = dist / f"onrdp-windows-{arch}.zip"
    with tempfile.TemporaryDirectory(dir=dist) as work:
        stage = Path(work)
        # The intermediate Onix archive becomes the published filename only
        # after its runtime files have been added.
        intermediate = stage / archive.name
        archive.replace(intermediate)
        with ZipFile(intermediate) as files:
            files.extractall(stage)
        app = stage / "onrdp"
        subprocess.run([sys.executable, ROOT / "scripts/bundle-rustdesk.py", f"windows-{arch}", app], check=True)
        subprocess.run([sys.executable, ROOT / "scripts/bundle-scrcpy.py", scrcpy_target, app], check=True)
        bundle_runtime(app, arch, host)
        packaged = Path(shutil.make_archive(str(stage / "release"), "zip", stage, app.name))
        packaged.replace(archive)
    with archive.open("rb") as file:
        digest = hashlib.file_digest(file, "sha256").hexdigest()
    (dist / "SHA256SUMS").write_text(f"{digest}  {archive.name}\n", encoding="ascii")
    print(archive)


if __name__ == "__main__":
    build()
