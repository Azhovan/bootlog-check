#!/usr/bin/env python3
"""Real timeout and interrupt checks. Never part of the offline Go verification."""
import argparse
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

from check import require
import qemu

HERE = Path(__file__).resolve().parent


def containers():
    result = subprocess.run(["docker", "ps", "-aq", "--filter", "label=bootlog-check=issue8"],
                            capture_output=True, text=True, check=True, timeout=20)
    return set(result.stdout.split())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("work", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=False)
    baseline = containers()
    # Reproduce interruption after daemon-side creation but before its client returns.
    original_docker = qemu.docker

    def interrupted_create(*arguments, **kwargs):
        result = original_docker(*arguments, **kwargs)
        if arguments[0] == "create":
            raise KeyboardInterrupt
        return result

    qemu.docker = interrupted_create
    try:
        try:
            qemu.boot(args.work.resolve(), args.output.resolve() / "create-interrupt", "hang")
            raise ValueError("creation interruption was not exercised")
        except KeyboardInterrupt:
            pass
    finally:
        qemu.docker = original_docker
    require(containers() == baseline, "creation interruption leaked a container")
    print("creation interrupt: daemon-created container removed")

    for operation in ("timeout", "interrupt", "terminate"):
        prefix = args.output.resolve() / operation
        argv = [sys.executable, str(HERE / "qemu.py"), "boot", str(args.work.resolve()),
                str(prefix), "hang", "--timeout", "2" if operation == "timeout" else "30"]
        process = subprocess.Popen(argv, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            if operation != "timeout":
                deadline = time.monotonic() + 20
                while time.monotonic() < deadline and process.poll() is None:
                    partial = prefix.with_suffix(".partial.log")
                    if partial.exists() and b"BOOTLOG_SCENARIO hang" in partial.read_bytes():
                        break
                    time.sleep(0.05)
                else:
                    raise ValueError("hanging guest did not start before interrupt")
                os.kill(process.pid, signal.SIGINT if operation == "interrupt" else signal.SIGTERM)
            stdout, stderr = process.communicate(timeout=40)
            require(process.returncode == 1, f"{operation}: unexpected runner exit {process.returncode}")
            require(b"QEMU example failed:" in stderr, f"{operation}: missing failure diagnostic")
            require(b"Captured " not in stdout, f"{operation}: false success announcement")
            require(b"BOOTLOG_SCENARIO hang" in prefix.with_suffix(".partial.log").read_bytes(),
                    f"{operation}: guest never ran")
            require(not prefix.with_suffix(".log").exists(), f"{operation}: partial log accepted")
            require(not prefix.with_suffix(".boot.json").exists(), f"{operation}: proof metadata emitted")
            require(containers() == baseline, f"{operation}: leaked container")
            print(f"{operation}: runner exit 1, partial capture rejected, no surviving container")
        finally:
            if process.poll() is None:
                process.send_signal(signal.SIGINT)
                process.communicate(timeout=40)


if __name__ == "__main__":
    main()
