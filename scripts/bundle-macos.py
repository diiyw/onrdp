#!/usr/bin/env python3
"""Bundle the complete dylib closure, seal it, and reject external dependencies."""
import json
import os
from pathlib import Path
import plistlib
import shutil
import subprocess
import sys


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def deps(path):
    entries = [line.strip().split(" (", 1)[0] for line in run("otool", "-L", str(path)).splitlines()[1:]]
    # otool includes a dylib's own install ID before its dependencies.
    return entries[1:] if path.suffix == ".dylib" else entries


def system(path):
    return path.startswith(("/System/Library/", "/usr/lib/"))


def bundle(app):
    app = app.resolve()
    with (app / "Contents/Info.plist").open("rb") as file:
        executable = plistlib.load(file)["CFBundleExecutable"]
    binary = app / "Contents/MacOS" / executable
    frameworks = app / "Contents/Frameworks"
    licenses = app / "Contents/Resources/NativeLicenses"
    frameworks.mkdir(exist_ok=True)
    licenses.mkdir(exist_ok=True)
    prefix = Path(os.environ["FREERDP_PREFIX"]).resolve()
    def resolve(origin, dep):
        if dep.startswith("@rpath/"):
            return (prefix / "lib" / dep.removeprefix("@rpath/")).resolve(strict=True)
        if dep.startswith("@loader_path/"):
            return (origin.parent / dep.removeprefix("@loader_path/")).resolve(strict=True)
        if dep.startswith("@executable_path/"):
            return (binary.parent / dep.removeprefix("@executable_path/")).resolve(strict=True)
        return Path(dep).resolve(strict=True)
    originals = {}
    pending = [binary]
    scanned = set()
    while pending:
        origin = pending.pop().resolve()
        if origin in scanned:
            continue
        scanned.add(origin)
        for dep in deps(origin):
            if system(dep):
                continue
            source = resolve(origin, dep)
            name = source.name
            if name in originals and originals[name] != source:
                raise RuntimeError(f"duplicate library basename: {source}")
            if name not in originals:
                originals[name] = source
                shutil.copy2(source, frameworks / name)
                (frameworks / name).chmod(0o755)
                pending.append(source)
    # Keep a traceable manifest and the upstream license texts for each package.
    packages = {}
    for source in originals.values():
        if source.is_relative_to(prefix):
            package = prefix
            name = "freerdp"
        elif "Cellar" in source.parts:
            index = source.parts.index("Cellar")
            package = Path(*source.parts[:index + 3])
            name = source.parts[index + 1]
        else:
            raise RuntimeError(f"no license source for {source}")
        if name in packages:
            continue
        if name == "freerdp":
            texts = list((prefix / "share/licenses/freerdp").glob("*"))
            metadata = {"version": "3.33.0", "license": "Apache-2.0", "source": "https://github.com/FreeRDP/FreeRDP/releases/tag/3.33.0"}
        else:
            info = json.loads(run("brew", "info", "--json=v2", name))["formulae"][0]
            metadata = {"version": package.name, "license": info["license"], "source": info["urls"]["stable"]["url"]}
            texts = [p for pattern in ("LICENSE*", "COPYING*", "NOTICE*", "share/doc/*/LICENSE*", "share/doc/*/COPYING*") for p in package.glob(pattern) if p.is_file()]
        if not texts:
            raise RuntimeError(f"missing license text for {name}")
        dest = licenses / name
        dest.mkdir(exist_ok=True)
        for text in texts:
            shutil.copy2(text, dest / text.name)
        packages[name] = metadata
    (licenses / "manifest.json").write_text(json.dumps(packages, indent=2) + "\n")
    outputs = [(binary, binary)] + [(source, frameworks / name) for name, source in originals.items()]
    for origin, output in outputs:
        changes = []
        for dep in deps(origin):
            if not system(dep):
                name = resolve(origin, dep).name
                relative = ("@executable_path/../Frameworks/" if output == binary else "@loader_path/") + name
                changes.extend(["-change", dep, relative])
        if output != binary:
            changes.extend(["-id", "@rpath/" + output.name])
        if changes:
            subprocess.check_call(["install_name_tool", *changes, str(output)])
    for lib in sorted(frameworks.iterdir()):
        subprocess.check_call(["codesign", "--force", "--sign", "-", str(lib)])
    subprocess.check_call(["codesign", "--force", "--sign", "-", str(app)])
    subprocess.check_call(["codesign", "--verify", "--deep", "--strict", str(app)])
    # A package may not reference the build machine's Homebrew or source paths.
    for _, output in outputs:
        for dep in deps(output):
            if system(dep):
                continue
            if dep.startswith("@executable_path/"):
                target = binary.parent / dep.removeprefix("@executable_path/")
            elif dep.startswith("@loader_path/"):
                target = output.parent / dep.removeprefix("@loader_path/")
            elif dep.startswith("@rpath/") and output != binary and dep == "@rpath/" + output.name:
                target = output
            else:
                raise RuntimeError(f"external dependency {dep} in {output}")
            if not target.is_file():
                raise RuntimeError(f"missing bundled dependency {target}")
    print(f"Bundled {len(originals)} libraries in {app}")


if __name__ == "__main__":
    bundle(Path(sys.argv[1]))
