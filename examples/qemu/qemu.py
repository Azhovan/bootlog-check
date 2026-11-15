#!/usr/bin/env python3
"""Build and capture the isolated ARM64 QEMU example. Requires a local ARM64 Docker engine."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import time
import urllib.request
import uuid

from check import CASES, expected_report, require

HERE = Path(__file__).resolve().parent
KERNEL = "linux-6.12.60.tar.xz"
KERNEL_URL = f"https://cdn.kernel.org/pub/linux/kernel/v6.x/{KERNEL}"
KERNEL_SHA = "a63096b2147411d683cecbf87622bb2ff4885bac2b3641d3d4f10250c89cdcf8"
BASE = "alpine@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b"
IMAGE = "bootlog-check-qemu:11.0.3-linux6.12.60"
INPUTS = ("Dockerfile", "packages.lock", "kernel.config", "init.c", "initramfs.list", "build.sh")
ARTIFACTS = ("Image", "initramfs.cpio", "kernel.config")


def sha(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def command(args, **kwargs):
    return subprocess.run(args, check=True, timeout=kwargs.pop("timeout", 120), **kwargs)


def docker(*args, **kwargs):
    return command(["docker", *args], **kwargs)


def docker_text(*args):
    return docker(*args, capture_output=True, text=True).stdout.strip()


def remove_container(identifier):
    # Removing the exact disposable container kills QEMU even if its attach client died.
    existing = docker_text("ps", "-aq", "--filter", f"name=^/{identifier}$")
    if existing:
        docker("rm", "--force", identifier, stdout=subprocess.DEVNULL, timeout=30)
    require(not docker_text("ps", "-aq", "--filter", f"name=^/{identifier}$"),
            f"container survived cleanup: {identifier}")


def download(url, digest, destination):
    if destination.exists():
        require(sha(destination) == digest, f"cached checksum mismatch: {destination}")
        return
    temporary = destination.with_suffix(destination.suffix + ".part")
    try:
        with urllib.request.urlopen(url, timeout=60) as response, temporary.open("wb") as target:
            shutil.copyfileobj(response, target)
        require(sha(temporary) == digest, f"download checksum mismatch: {url}")
        temporary.replace(destination)
    finally:
        temporary.unlink(missing_ok=True)


def prepare(work):
    started = time.monotonic()
    require(docker_text("info", "--format", "{{.Architecture}}") in ("aarch64", "arm64"),
            "this recipe requires a local ARM64 Docker engine")
    work.mkdir(parents=True, exist_ok=True)
    require(not (work / "build.json").exists(), "build already exists; use a new work directory")
    inputs = work / "inputs"
    inputs.mkdir(exist_ok=True)
    for name in INPUTS:
        shutil.copyfile(HERE / name, inputs / name)
    input_hashes = {name: sha(inputs / name) for name in INPUTS}
    context = work / "docker-context"
    packages = context / "packages"
    packages.mkdir(parents=True, exist_ok=True)
    pins = [line.split() for line in (inputs / "packages.lock").read_text().splitlines()]
    expected_names = {url.rsplit("/", 1)[1] for _, url in pins}
    require(not {p.name for p in packages.iterdir()} - expected_names,
            "unexpected files in package cache; use a clean cache")
    for digest, url in pins:
        download(url, digest, packages / url.rsplit("/", 1)[1])
    download(KERNEL_URL, KERNEL_SHA, work / KERNEL)
    shutil.copyfile(inputs / "Dockerfile", context / "Dockerfile")
    docker("build", "--platform", "linux/arm64", "--tag", IMAGE, str(context), timeout=900)
    image_id = docker_text("image", "inspect", IMAGE, "--format", "{{.Id}}")
    identifier = "bootlog-qemu-build-" + uuid.uuid4().hex[:12]
    try:
        docker("create", "--name", identifier, "--network", "none",
               "--label", "bootlog-check=issue8", image_id, "sh", "/tmp/build.sh",
               stdout=subprocess.DEVNULL)
        for name in ("kernel.config", "init.c", "initramfs.list", "build.sh"):
            docker("cp", str(inputs / name), f"{identifier}:/tmp/{name}")
        docker("cp", str(work / KERNEL), f"{identifier}:/tmp/{KERNEL}")
        docker("start", "--attach", identifier, timeout=900)
        require(docker_text("inspect", identifier, "--format", "{{.State.ExitCode}}") == "0",
                "kernel/initramfs build failed")
        for name in ARTIFACTS:
            docker("cp", f"{identifier}:/work/{name}", str(work / name))
        docker("cp", f"{identifier}:/work/build.log", str(work / "build.log"))
    finally:
        remove_container(identifier)
    require(input_hashes == {name: sha(HERE / name) for name in INPUTS},
            "recipe inputs changed during build; rebuild before capturing")
    require(sha(work / "kernel.config") == input_hashes["kernel.config"], "resolved config changed")
    details = {
        "base_image": BASE, "image_id": image_id, "kernel_url": KERNEL_URL,
        "kernel_sha256": KERNEL_SHA,
        "inputs": input_hashes,
        "artifacts": {name: sha(work / name) for name in ARTIFACTS},
    }
    (work / "build.json").write_text(json.dumps(details, indent=2) + "\n")
    print(f"Prepared {work} in {time.monotonic() - started:.1f} host seconds", flush=True)


def load_build(work):
    details = json.loads((work / "build.json").read_text())
    require(details["inputs"] == {name: sha(HERE / name) for name in INPUTS},
            "recipe inputs changed; rebuild before capturing")
    require(details["artifacts"] == {name: sha(work / name) for name in ARTIFACTS},
            "built artifact checksum mismatch")
    require(docker_text("image", "inspect", details["image_id"], "--format", "{{.Architecture}}") == "arm64",
            "wrong build image architecture")
    return details


def qemu_arguments(scenario):
    return ["qemu-system-aarch64", "-machine", "virt-11.0,gic-version=2", "-cpu", "cortex-a57",
            "-accel", "tcg,thread=single", "-smp", "1", "-m", "128M", "-display", "none",
            "-monitor", "none", "-serial", "stdio", "-no-reboot", "-nic", "none",
            "-kernel", "/tmp/Image", "-initrd", "/tmp/initramfs.cpio", "-append",
            "console=ttyAMA0 printk.time=1 loglevel=8 printk.devkmsg=on rdinit=/init "
            f"bootlog.scenario={scenario}"]


def boot(work, prefix, scenario, timeout=45):
    details = load_build(work)
    prefix.parent.mkdir(parents=True, exist_ok=True)
    partial = prefix.with_suffix(".partial.log")
    stderr = prefix.with_suffix(".stderr")
    final = prefix.with_suffix(".log")
    metadata = prefix.with_suffix(".boot.json")
    require(not any(p.exists() for p in (partial, stderr, final, metadata)),
            "capture paths already exist; choose a new output directory")
    args = qemu_arguments(scenario)
    identifier = "bootlog-qemu-boot-" + uuid.uuid4().hex[:12]
    try:
        docker("create", "--name", identifier, "--network", "none", "--user", "65534:65534",
               "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
               "--label", "bootlog-check=issue8", details["image_id"], *args,
               stdout=subprocess.DEVNULL)
        for name in ("Image", "initramfs.cpio"):
            docker("cp", str(work / name), f"{identifier}:/tmp/{name}")
        with partial.open("xb") as output, stderr.open("xb") as errors:
            docker("start", "--attach", identifier, stdout=output, stderr=errors, timeout=timeout)
        require(docker_text("inspect", identifier, "--format", "{{.State.ExitCode}}") == "0",
                "QEMU exited abnormally")
        require(not stderr.read_bytes(), "QEMU emitted stderr; inspect the partial capture")
        expected_report(partial, scenario)
    finally:
        remove_container(identifier)
    # A timeout, signal, abnormal exit, or incomplete capture never receives a .log name.
    partial.replace(final)
    metadata.write_text(json.dumps({"scenario": scenario, "qemu_argv": args, "qemu_exit": 0,
                                    "image_id": details["image_id"], "log_sha256": sha(final),
                                    "artifacts": details["artifacts"]}, indent=2) + "\n")
    print(f"Captured {final}", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="action", required=True)
    setup = commands.add_parser("prepare", help="download pinned inputs and build in disposable containers")
    setup.add_argument("work", type=Path)
    capture = commands.add_parser("capture", help="execute all six required real boots")
    capture.add_argument("work", type=Path)
    capture.add_argument("output", type=Path)
    one = commands.add_parser("boot", help="one boot, including the hang scenario for lifecycle tests")
    one.add_argument("work", type=Path)
    one.add_argument("prefix", type=Path)
    one.add_argument("scenario", choices=("normal", "missing", "forbidden", "delayed", "hang"))
    one.add_argument("--timeout", type=float, default=45)
    args = parser.parse_args()
    if args.action == "prepare":
        prepare(args.work.resolve())
    elif args.action == "capture":
        require(not args.output.exists(), "capture output directory must be new")
        args.output.mkdir(parents=True)
        for case in CASES:
            boot(args.work.resolve(), args.output.resolve() / case, case.split("-")[0])
    else:
        require(args.timeout > 0, "timeout must be positive")
        boot(args.work.resolve(), args.prefix.resolve(), args.scenario, args.timeout)


if __name__ == "__main__":
    def terminated(signum, _frame):
        raise InterruptedError(f"signal {signum}")

    signal.signal(signal.SIGTERM, terminated)
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError, KeyboardInterrupt) as error:
        print(f"QEMU example failed: {error or 'interrupted'}", file=sys.stderr)
        sys.exit(1)
