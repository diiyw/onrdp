#!/usr/bin/env python3
"""Build the scrcpy shared library and bundle it with the matching server and adb."""

import argparse
import hashlib
import os
from pathlib import Path
import sys
import shlex
import shutil
import subprocess
import tempfile
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parent.parent
CACHE = ROOT / ".native" / "scrcpy"
VERSION = "5.0.1"
ASSETS = {
    "linux-x86_64": "9f969d30cc574816077edecda65719c1c70b0aee1df1ba5fa00779ac07b8fd6e",
    "macos-aarch64": "33611e51977a8289e2e124b220f727895181ef2eb9060e0987a4dc2db9cf0d6b",
    "macos-x86_64": "31a5467a9e907f162b093267cbbab1276b15c6e709c2ea8159566a8d165beb50",
    "win64": "b12a2c4ee8be317422451fc7dcf8ee20a71b5ea7ef9ad73ddd825a25316227a5",
    "winarm64": "16c3fc2068df64946f670e3504b4752073ebae47a416a45b46e79a5ab19c689b",
}
FFMPEG = "9.0.2"
FFMPEG_HASH = "8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e"


def download(path, url, digest):
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.is_file() and hashlib.sha256(path.read_bytes()).hexdigest() == digest:
        return path
    with tempfile.TemporaryDirectory(dir=path.parent) as work:
        incoming = Path(work) / path.name
        with urlopen(url, timeout=120) as response, incoming.open("wb") as output:
            shutil.copyfileobj(response, output)
        if hashlib.sha256(incoming.read_bytes()).hexdigest() != digest:
            raise ValueError(f"SHA-256 mismatch for {path.name}")
        incoming.replace(path)
    return path


def run(*args, **kwargs):
    subprocess.run(args, check=True, **kwargs)


def build_library(target):
    archive = download(CACHE / f"ffmpeg-{FFMPEG}.tar.xz",
                       f"https://ffmpeg.org/releases/ffmpeg-{FFMPEG}.tar.xz", FFMPEG_HASH)
    source = CACHE / f"ffmpeg-{FFMPEG}"
    if not source.is_dir():
        with tempfile.TemporaryDirectory(dir=CACHE) as tmp:
            shutil.unpack_archive(archive, tmp)
            Path(tmp, source.name).rename(source)

    cc = os.environ.get("CC", "cc")
    options = ["--disable-all", "--disable-autodetect", "--disable-doc", "--enable-pic",
               "--enable-avcodec", "--enable-swscale", "--enable-decoder=h264"]
    link = ["-shared"]
    files = [ROOT / "native/scrcpy/decoder.c"]
    includes = []
    suffix = ".so"
    if target.startswith("linux"):
        options += ["--enable-vaapi", "--enable-libdrm", "--enable-hwaccel=h264_vaapi"]
        files.append(ROOT / "native/scrcpy/vaapi_shim.c")
        includes = shlex.split(subprocess.check_output(
            ["pkg-config", "--cflags", "libva-drm", "libdrm"], text=True))
        link += ["-Wl,--exclude-libs,ALL", "-Wl,-z,defs"]
    elif target.startswith("win"):
        suffix = ".dll"
        options += ["--enable-d3d11va", "--enable-hwaccel=h264_d3d11va,h264_d3d11va2"]
        link += ["-static-libgcc"]
        if sys.platform != "win32" and not os.environ.get("MSYSTEM"):
            arch = "aarch64" if target == "winarm64" else "x86_64"
            prefix = arch + "-w64-mingw32-"
            cc = prefix + "gcc"
            options += ["--enable-cross-compile", "--target-os=mingw32",
                        "--arch=" + arch, "--cross-prefix=" + prefix]
    else:
        suffix = ".dylib"
        options += ["--enable-videotoolbox", "--enable-hwaccel=h264_videotoolbox"]
        link = ["-dynamiclib", "-Wl,-install_name,@rpath/libonrdp-scrcpy.dylib"]
        if sys.platform != "darwin":
            arch = target.split("-")[1]
            options += ["--enable-cross-compile", "--target-os=darwin", "--arch=" + arch,
                        "--ar=" + os.environ["AR"], "--ranlib=" + os.environ["RANLIB"],
                        "--nm=" + os.environ.get("NM", "llvm-nm"),
                        "--strip=" + os.environ.get("STRIP", "llvm-strip")]

    options += ["--cc=" + cc, "--pkg-config=pkg-config"]
    digest = hashlib.sha256(repr(options).encode()).hexdigest()[:12]
    build = CACHE / f"decoder-{FFMPEG}-{target}-{digest}"
    build.mkdir(exist_ok=True)
    install = build / "install"
    if not (build / "ffbuild/config.mak").exists():
        configure = source / "configure"
        prefix = str(install)
        if sys.platform == "win32" and os.environ.get("MSYSTEM"):
            configure, prefix = subprocess.check_output(
                ["cygpath", "-u", str(configure), prefix], text=True).splitlines()
        run("bash", configure, "--prefix=" + prefix, *options, cwd=build)
    run("make", "-j" + os.environ.get("CMAKE_BUILD_PARALLEL_LEVEL", "8"), "install", cwd=build)
    env = dict(os.environ, PKG_CONFIG_LIBDIR=str(install / "lib/pkgconfig"), PKG_CONFIG_PATH="")
    flags = shlex.split(subprocess.check_output(
        ["pkg-config", "--cflags", "--libs", "--static", "libavcodec", "libswscale"], env=env, text=True))
    if target.startswith("linux"):
        flags = [f for f in flags if f not in ("-lva", "-lva-drm", "-ldrm")]
        flags += ["-ldl"]
    if target.startswith("macos"):
        link += ["-Wl,-exported_symbol,_onrdp_scrcpy_*"]
    binary = build / ("libonrdp-scrcpy" + suffix)
    run(cc, "-O2", "-fPIC", "-fvisibility=hidden", *link, "-o", binary, *includes, *files, *flags,
        *shlex.split(os.environ.get("LDFLAGS", "")))
    return binary, {"FFmpeg-LGPL.txt": source / "COPYING.LGPLv2.1"}


def bundle(target, directory):
    name = f"scrcpy-{target}-v{VERSION}"
    filename = name + (".zip" if target.startswith("win") else ".tar.gz")
    archive = download(CACHE / filename,
                       f"https://github.com/Genymobile/scrcpy/releases/download/v{VERSION}/{filename}", ASSETS[target])
    binary, licenses = build_library(target)
    with tempfile.TemporaryDirectory() as temporary:
        shutil.unpack_archive(archive, temporary)
        release = Path(temporary) / name
        dest = directory / "tools" / "scrcpy"
        dest.mkdir(parents=True, exist_ok=True)
        adb = ["adb.exe", "AdbWinApi.dll", "AdbWinUsbApi.dll"] if target.startswith("win") else ["adb"]
        for path in release.iterdir():
            if path.name in adb or path.name == "scrcpy-server" or path.name.lower().startswith(("license", "notice")):
                if path.is_dir():
                    shutil.copytree(path, dest / path.name, dirs_exist_ok=True)
                else:
                    shutil.copy2(path, dest)
        for required in (*adb, "scrcpy-server"):
            if not (dest / required).is_file():
                raise FileNotFoundError(f"Missing scrcpy release asset: {required}")
        shutil.copy2(binary, dest)
        for name, path in licenses.items():
            shutil.copy2(path, dest / name)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("target", choices=ASSETS)
    parser.add_argument("directory", type=Path)
    args = parser.parse_args()
    bundle(args.target, args.directory)
