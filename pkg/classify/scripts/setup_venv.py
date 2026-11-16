#!/usr/bin/env python3
"""Make pkg/classify/.venv, with the right build of PyTorch in it.

    python3 pkg/classify/scripts/setup_venv.py            # make it
    python3 pkg/classify/scripts/setup_venv.py --dry-run  # say what it would do

Needs only the standard library, so any python 3.10-3.14 can run it (that is
what PyTorch supports: https://pytorch.org/get-started/locally/). The venv is
made with the python that runs this, or --python.

Which PyTorch? The install page is a selector that needs a browser, so this
does what the selector does, from what is on the machine and what PyTorch has
published:

  macOS      the plain PyPI wheels, which have Apple's GPU (MPS) built in.
  NVIDIA     the newest CUDA build of PyTorch whose CUDA is no newer than what
             the driver supports (the "CUDA Version" nvidia-smi prints), that
             also has a wheel for this python and platform.
  AMD        (Linux) the newest ROCm build with a wheel, if ROCm is installed.
  otherwise  the CPU build. (On Linux PyPI's default wheel is the CUDA one and
             is gigabytes bigger, so the CPU index is used.)

The builds on offer are read from https://download.pytorch.org/whl/ when this
runs, not from a table here, because the list changes with every release.
--device overrides the guess, and --toolkit 12.8 caps the CUDA/ROCm version.
"""
import argparse
import os
import platform
import re
import shutil
import subprocess
import sys
import urllib.request
import venv
from pathlib import Path
from urllib.parse import unquote

WHL = "https://download.pytorch.org/whl/"
HERE = Path(__file__).resolve().parent
PKG = HERE.parent


def say(*a):
    print("setup_venv:", *a, flush=True)


def fetch(url):
    req = urllib.request.Request(url, headers={"User-Agent": "monet-classify-setup"})
    with urllib.request.urlopen(req, timeout=30) as r:
        return r.read().decode("utf-8", "replace")


def vkey(v):
    return tuple(int(p) for p in v.split("."))


def detect_device():
    """What this machine can run PyTorch on: mps, cuda, rocm or cpu, and the
    newest version of the toolkit it supports, as a tuple, when that matters."""
    system, machine = platform.system(), platform.machine().lower()
    if system == "Darwin":
        return ("mps" if machine in ("arm64", "aarch64") else "cpu"), None
    smi = shutil.which("nvidia-smi")
    if smi:
        try:
            out = subprocess.run([smi], capture_output=True, text=True, timeout=30).stdout
            m = re.search(r"CUDA Version:\s*(\d+)\.(\d+)", out)
            if m:
                return "cuda", (int(m.group(1)), int(m.group(2)))
        except (OSError, subprocess.SubprocessError):
            pass
    if system == "Linux" and (shutil.which("rocminfo") or Path("/opt/rocm").exists()):
        ver = Path("/opt/rocm/.info/version")
        m = re.match(r"(\d+)\.(\d+)", ver.read_text()) if ver.exists() else None
        return "rocm", ((int(m.group(1)), int(m.group(2))) if m else None)
    return "cpu", None


def platform_wheel_tags():
    """Substrings of a wheel's platform tag that this machine can use."""
    system, machine = platform.system(), platform.machine().lower()
    if system == "Linux":
        return ["manylinux"], ["aarch64"] if machine in ("aarch64", "arm64") else ["x86_64"]
    if system == "Windows":
        return ["win_amd64"], []
    if system == "Darwin":
        return ["macosx"], ["arm64"] if machine in ("arm64", "aarch64") else ["x86_64"]
    return [], []


def best_torch(variant, pytag):
    """The newest torch on the index for this variant that has a wheel for this
    python and platform, or None."""
    try:
        page = fetch("%s%s/torch/" % (WHL, variant))
    except Exception as e:  # a variant that has no torch page, or no network
        say("could not read the %s index: %s" % (variant, e))
        return None
    plat_any, plat_arch = platform_wheel_tags()
    best = None
    for href in re.findall(r'href="([^"]+\.whl)', page):
        name = unquote(href.split("/")[-1].split("#")[0])
        m = re.match(r"torch-(\d+(?:\.\d+)+)(?:\+[\w.]+)?-(cp\d+)-(cp\d+t?)-(.+)\.whl$", name)
        if not m:
            continue
        ver, py, abi, plat = m.groups()
        if py != pytag or abi != pytag:  # not the free-threaded build
            continue
        if not any(p in plat for p in plat_any) or not all(p in plat for p in plat_arch):
            continue
        if best is None or vkey(ver) > vkey(best):
            best = ver
    return best


def variants(listing, prefix):
    """(version tuple, directory name) of the cuNNN / rocmX.Y builds on the index."""
    out = []
    for d in re.findall(r'href="([^"/]+)/"', listing):
        if prefix == "cu":
            m = re.fullmatch(r"cu(\d+)", d)
            if m and len(m.group(1)) >= 3:  # cu118, cu130; not the ancient cu92
                digits = m.group(1)
                out.append(((int(digits[:-1]), int(digits[-1])), d))
        else:
            m = re.fullmatch(r"rocm(\d+)\.(\d+)(?:\.(\d+))?", d)
            if m:
                out.append((tuple(int(x) for x in m.groups() if x), d))
    return sorted(out, reverse=True)


def choose(device, limit, pytag):
    """The index to install from and the torch version it will give: (url or
    None, label, version or None)."""
    if device == "mps":
        return None, "PyPI (Apple GPU, MPS, is built in)", None
    listing = fetch(WHL)
    if device in ("cuda", "rocm"):
        cands = variants(listing, "cu" if device == "cuda" else "rocm")
        if limit:
            cands = [c for c in cands if c[0][:2] <= limit[:2]]
        # newest toolkit first, but a newer toolkit with only an older torch
        # loses to an older toolkit with the newest torch
        best = None
        for _, name in cands[:6]:
            ver = best_torch(name, pytag)
            if ver and (best is None or vkey(ver) > vkey(best[1])):
                best = (name, ver)
        if best:
            return WHL + best[0], best[0], best[1]
        say("WARNING: no %s build of torch for this python and platform%s; using the CPU build instead. "
            "(Update the driver, or make the venv with an older python: --python python3.12)"
            % (device, " at or below toolkit %d.%d" % limit[:2] if limit else ""))
    ver = best_torch("cpu", pytag)
    if platform.system() == "Windows" and device == "cpu":
        return None, "PyPI (its Windows wheel is the CPU one)", None
    return WHL + "cpu", "cpu", ver


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--venv", default=str(PKG / ".venv"))
    ap.add_argument("--python", help="the python to make the venv with (default: the one running this)")
    ap.add_argument("--device", choices=["auto", "cpu", "cuda", "rocm", "mps"], default="auto")
    ap.add_argument("--toolkit", metavar="X.Y", help="for cuda/rocm: the newest toolkit to use, when it is not what the driver says")
    ap.add_argument("--dry-run", action="store_true", help="say what would be done, and do nothing")
    ap.add_argument("--no-torch", action="store_true", help="only the other requirements")
    args = ap.parse_args()

    py = args.python or sys.executable
    out = subprocess.run([py, "-c", "import sys;print(*sys.version_info[:3])"], capture_output=True, text=True)
    major, minor, micro = (int(x) for x in out.stdout.split())
    if (major, minor) < (3, 10) or (major, minor) > (3, 14):
        sys.exit("setup_venv: python %d.%d.%d is outside what PyTorch supports (3.10-3.14); use --python" % (major, minor, micro))
    pytag = "cp%d%d" % (major, minor)

    device, limit = detect_device()
    if args.device != "auto":
        device, limit = args.device, None
    if args.toolkit:
        limit = tuple(int(x) for x in args.toolkit.split(".")[:2])
    say("python %d.%d.%d (%s), device: %s%s" % (major, minor, micro, pytag, device, " (toolkit %d.%d)" % limit if limit else ""))

    venv_dir = Path(args.venv)
    vpy = venv_dir / ("Scripts/python.exe" if os.name == "nt" else "bin/python")
    torch_cmd = None
    if not args.no_torch:
        url, label, ver = choose(device, limit, pytag)
        pin = "torch==%s" % ver if ver else "torch"
        torch_cmd = [str(vpy), "-m", "pip", "install", pin, "torchvision"] + (["--index-url", url] if url else [])
        say("pytorch from %s%s" % (label, ", version %s" % ver if ver else ""))
    reqs = [str(vpy), "-m", "pip", "install", "-r", str(PKG / "requirements.txt")]

    if args.dry_run:
        say("would make", venv_dir, "with", py)
        for c in (torch_cmd, reqs):
            if c:
                say("would run:", " ".join(c))
        return

    if not vpy.exists():
        say("making", venv_dir)
        if args.python:
            subprocess.check_call([py, "-m", "venv", str(venv_dir)])
        else:
            venv.EnvBuilder(with_pip=True).create(venv_dir)
    for c in (torch_cmd, reqs):
        if c:
            say("running:", " ".join(c))
            subprocess.check_call(c)
    subprocess.check_call([str(vpy), "-c", (
        "import torch;print('torch', torch.__version__, '| cuda', torch.cuda.is_available(),"
        " '| mps', torch.backends.mps.is_available())")])
    say("done. use %s" % vpy)


if __name__ == "__main__":
    main()
