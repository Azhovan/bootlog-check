# Checking saved Linux boots from QEMU

These six saved serial logs came from Linux boots under QEMU software emulation
on 2026-11-15. The normal boots pass the configured checks. The scenarios with
missing, forbidden, and deliberately delayed guest behavior produce the expected
failures. This demonstrates that the checker handles these captures correctly.
It does not establish firmware health, capture completeness, hardware timing,
or statistical significance.

The capture scripts belong to this example. The library and CLI read saved
files; they never start QEMU, containers, or guest commands.

## Replay the saved evidence offline

From the repository root, with the Go version in `.go-version` and Python 3.11+
installed:

```sh
go build -o ./bin/bootlog-check ./cmd/bootlog-check
python3 examples/qemu/check.py --cli ./bin/bootlog-check --captures examples/qemu/captures
python3 examples/qemu/test_oracle.py
```

For each capture, `check.py` runs the CLI twice, checks its exit code and confirms
that stderr is empty. It derives the expected JSON fields independently from the
saved log, compares every field, and requires both runs to produce identical bytes
matching the saved report. Any mismatch returns a nonzero exit code.

This replay needs no Docker, QEMU, downloads, root access, or hardware. The regular
`./scripts/verify.sh` also runs offline and does not start new QEMU boots.

## Run new boots

You need Python 3.11+, Go to build the native CLI, and a running local Linux ARM64
Docker engine. The original execution used macOS ARM64 with Colima's
Linux/aarch64 Ubuntu 24.04.4 VM, two CPUs, Docker client 29.5.0 and server 29.2.1,
Python 3.15.2, and Go 1.27.4. A native ARM64 Linux Docker host can use the same
instructions. The scripts reject x86 Docker engines. If you need to install a
VM or Docker engine, do that before starting this example.

Initial setup downloads about 141 MiB of Linux source, 138 MiB of pinned Alpine
packages, and a small base container image if it is not already present. Allow
roughly 4 GB of free disk space, including the 1.7 GB extracted kernel and build
tree and approximately 554 MiB of installed build tools. The two clean builds
recorded here took 170.0 and 126.7 seconds on the host using cached, verified
downloads. Download time and emulation speed vary.

Setup needs network access to kernel.org, Alpine, and the base image registry.
Subsequent builds and boots use cached inputs in containers with networking
disabled.

From the repository root, build the CLI and use a new directory outside the
checkout for generated inputs and outputs:

```sh
go build -o ./bin/bootlog-check ./cmd/bootlog-check
work=$(mktemp -d "${TMPDIR:-/tmp}/bootlog-qemu.XXXXXX")
python3 examples/qemu/qemu.py prepare "$work"
python3 examples/qemu/qemu.py capture "$work" "$work/captures"
python3 examples/qemu/check.py --cli ./bin/bootlog-check --captures "$work/captures" --write-reports
python3 examples/qemu/check.py --cli ./bin/bootlog-check --captures "$work/captures"
python3 examples/qemu/test_lifecycle.py "$work" "$work/lifecycle"
```

To reuse cached downloads, copy the Linux tarball and the 119 APKs from
`docker-context/packages/*.apk` to the same relative paths in a new work
directory. `prepare` checks their hashes before rebuilding the kernel and
initramfs. It rejects unpinned files and checksum mismatches. Use fresh paths
for build and capture outputs.

`prepare` copies the build inputs into a snapshot, builds the container image with
the pinned tools, and compiles the kernel using the full saved configuration. It
also builds a statically linked `/init` program. Compilation runs in a disposable
container. The script records input and artifact hashes in `build.json` outside
the checkout and checks those hashes again before booting.

The initramfs contains only `/init`, `/proc`, `/dev/console`, and `/dev/kmsg`,
created by Linux's `gen_init_cpio` from [initramfs.list](initramfs.list).

The build uses root only inside its isolated container. QEMU runs with user and
group ID 65534, all container capabilities dropped, and networking disabled.
It does not use KVM, a privileged container, host device nodes, or root commands
on the host.

For the normal scenario, `qemu.py` starts QEMU with this command:

```sh
qemu-system-aarch64 \
  -machine virt-11.0,gic-version=2 -cpu cortex-a57 \
  -accel tcg,thread=single -smp 1 -m 128M \
  -display none -monitor none -serial stdio -no-reboot -nic none \
  -kernel /tmp/Image -initrd /tmp/initramfs.cpio \
  -append 'console=ttyAMA0 printk.time=1 loglevel=8 printk.devkmsg=on rdinit=/init bootlog.scenario=normal'
```

The other scenario values are `missing`, `forbidden`, and `delayed`. The
`capture` command runs normal and delayed twice each, and the other two once each.
The host selects the scenario through a kernel command-line parameter;
[init.c](init.c) implements the behavior inside the guest.

Each marker is written to `/dev/kmsg` in one complete write. Marker timestamps
therefore use the same kernel clock as the single Linux-version message that
establishes boot start. The normal scenario waits one guest second before
readiness; delayed waits ten seconds. Missing never emits readiness. Forbidden
waits another second after readiness before writing the fault marker. The host
does not insert records into the log or change measured timestamps.

## Observed results and independent arithmetic

All six QEMU processes exited with code 0, emitted exactly one `BOOTLOG_COMPLETE`,
and ended with `reboot: Power down`. Every captured line was valid UTF-8 and had
a timestamp. Timestamps stayed the same or increased throughout each capture.
Boot start was line 2 at timestamp zero in all six logs, so elapsed times are
measured from that point. Readiness must occur within 5,000,000,000 ns; a match
exactly at that deadline would pass.

| Capture | Ready line | Observed ready elapsed ns | CLI exit | Expected failed check |
| --- | ---: | ---: | ---: | --- |
| [normal-1](captures/normal-1.log) | 81 | 1,160,497,000 | 0 | None |
| [normal-2](captures/normal-2.log) | 81 | 1,143,841,000 | 0 | None |
| [missing](captures/missing.log) | Absent | null | 1 | `ready: missing` |
| [forbidden](captures/forbidden.log) | 81 | 1,131,841,000 | 1 | `fault: forbidden_match` |
| [delayed-1](captures/delayed-1.log) | 81 | 10,151,326,000 | 1 | `ready: late` |
| [delayed-2](captures/delayed-2.log) | 81 | 10,134,627,000 | 1 | `ready: late` |

The first forbidden match is line 82 at 2,135,469,000 ns, after readiness. Each
normal report has three passed checks. Each other report has two passed checks
and the failed check shown above. None has a skipped check.

For example, `delayed-1.log` line 81 is the unmodified serial line:

```text
[   10.151326] BOOTLOG_READY
```

Converting the timestamp to nanoseconds gives
`10 * 1,000,000,000 + 151326 * 1,000 - 0 = 10,151,326,000 ns`.
That exceeds `5000 * 1,000,000 = 5,000,000,000 ns` by 5,151,326,000 ns.
For `normal-1`, `1,160,497,000 - 0 <= 5,000,000,000` passes.
The separate Python standard-library [checker](check.py) performs this integer
conversion for every physical line and constructs the complete expected report
from fixed known patterns without importing or invoking the product evaluator
to derive expectations. The product binary is invoked only for comparison.

The `.log` files contain the entire serial capture, including original CRLF
bytes; no truncation, filtering, sanitization, or timestamp editing was applied.
Only the fixed build user/host strings were selected before compiling Linux.
The matching `.json` files are unmodified actual CLI stdout. Git attributes
preserve raw log bytes across checkouts. Host stderr was empty for all six boots.
Exact measured times may vary on new boots; repeated reports for the same saved
capture must remain identical. These two repetitions are not a timing baseline.

## Failure handling and cleanup

Each boot gets a uniquely named disposable container and a 45-second timeout
measured by the host. The wrapper removes its container in a `finally` block,
including after a timeout, SIGINT, SIGTERM, or failure during creation.

A run is rejected if QEMU exits nonzero, writes to stderr, omits the completion or
poweroff markers, produces malformed serial output, or runs the wrong scenario.
Rejected runs leave only `.partial.log` diagnostic data. They do not receive a
final `.log` filename or proof metadata and must not be used as successful
captures.

The lifecycle test was executed and passed all four cases: interruption
immediately after daemon-side creation, a running `hang` guest with a two-second
timeout, SIGINT after its guest marker, and SIGTERM after its guest marker. Each
running-guest case returned wrapper exit 1, rejected the partial capture, and left
no surviving container.

The offline tests of the independent checker also passed. They cover the saved
reports, truncated or malformed tails, wrong scenarios, guest errors, ambiguous
JSON values, and tampered evidence, counts, timing, and summaries.

Normal completion and failure cleanup remove only their own named containers.
The external work directory and tools image are retained for reuse. After use,
delete the directory you created and optionally run
`docker image rm bootlog-check-qemu:11.0.3-linux6.12.60`.
If the runner is killed with uncatchable SIGKILL or the Docker daemon becomes
unreachable, inspect `docker ps -a --filter label=bootlog-check=issue8` and remove
the exact abandoned example container by name once the daemon is available.

## Pinned inputs and execution details

The six captures above were recorded on 2026-11-15, before the example was
integrated. The CLI was built with Go 1.27.4 from product source revision
`9c5bcc25c184a3e0d3162bcfee87bf75be7033c4`. The capture version of `qemu.py`
differs from the integrated script only in the check for extra cache files.
The inputs, kernel, initramfs, CLI, and raw captures are identified by SHA-256
below.

The kernel was built from the official Linux 6.12.60 source archive. The base
image is
`alpine@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b`.
[packages.lock](packages.lock) pins all 119 downloaded APK URLs and SHA-256 values,
including QEMU 11.0.3-r0, GCC 15.2.0-r5, binutils 2.45.1-r1, musl 1.2.6-r3,
and Make 4.4.1-r4. The executed local tools-image ID was
`sha256:7ff8a0f03a44e3c4af2beaf24f0bb195fcf1cc8ba370ca33a0c49cdce2732774`.
Kernel build user, host, timestamp, and build number are fixed in `build.sh`;
the resolved kernel config is committed in full. No third-party binaries or
build trees are stored in this repository.

The following SHA-256 checksums identify the inputs and built artifacts. Paths
are relative to this example unless marked external:

```text
a63096b2147411d683cecbf87622bb2ff4885bac2b3641d3d4f10250c89cdcf8  linux-6.12.60.tar.xz (external)
038d3008cf5f5eb493450174c1cdaf9a6906dd6ec53b8096b2215e438d4cd07e  kernel.config
84eec89cbeeb14844c257391c8f41c9b83a4c6164a68bfcc66dab49071609ca2  init.c
eb0957d680731c1d30d8516bd2dba4ca4a07c544af36a2dab02391fc09478f4b  initramfs.list
05259233a6c2aba34bc5d770d4bce64d21e730f8a28892bf2deaa60b3f90361b  build.sh
91221a5d37a55207a37a6206a60db7894eec507d178504e1c6d01a32a4c84545  Dockerfile
80f5075d4b2cb82e6895f953633d827bf643a35b512501715def23e8a1be78de  packages.lock
7aca917fe14306006301d5dddbeacce6c46e6cba1825e3ec629e3e0f02eff3f8  qemu.py (capture revision)
90cce04de99a6e90f3905195ca5bbeaf81619966122f1b9b80e3d2cd9337f2a5  check.py
19b334f003b4221cd5951152d5337a9c9b3528091947ac8749fc8ce477115e38  rules.json
deaa0e3ecee9af553b33d0fbc6d144c5aef553d093af1edabf738af5a75dd642  test_lifecycle.py
f3c4b40b858998d67a679f6511e90bfe81a6d342d75fe35e9e3d1abe529a1a34  Image (external)
2d7c2291c9903a21acafe34c8abfd335a30a6c02fe3b4b3299f78cb82720c1b6  initramfs.cpio (external)
dc49af5e5902f82725ee3fd562b8df5573e0cb75285eb53e336d18ace0cba6a8  native bootlog-check (external)
```

Both clean builds produced the same kernel and initramfs hashes shown above.
That was observed with these pinned tools; it does not guarantee identical
container image IDs or host binaries on other systems.

SHA-256 of the preserved raw captures and actual reports:

```text
492346f030d854cd7a302a4c32a64b45eb0e6ccb5b4e05ebc40e0f0a0693cedc  captures/normal-1.log
1a5ce999065de3c318173875d80b3dc865f1532683fdd6b575d9d77675ada987  captures/normal-1.json
beac001abe340b5ca9f7474d408b0762805df6e2c0cca1749a93da4a63e83b4f  captures/normal-2.log
44935b5df940ebdc20a89d268ec3334f70545cf62efdf25128448119fb8eb990  captures/normal-2.json
4e205f69eb470c2fc5fb3cdabf0684adf2612fa55096dabba7341f9d3be38276  captures/missing.log
d5092a103660c30630a4f0352304e2df5f0cfb57485cabf547ca8bf6b0bdac3b  captures/missing.json
8ef56fc8a7f726b3dd0f509b5acf37c11b2a7647e9f30ad1b350c89b30e7844f  captures/forbidden.log
7179c945947143addb349b14a36950e532e706189ccc6555447cd105d4a23e00  captures/forbidden.json
e361a213524a3807d8a48cda4523e32b05b65b8ca47eff7902820146cfe607da  captures/delayed-1.log
079e00b391dff008b6baa01633d5e0e4dede9dbac8e8aed30945be99a2f699eb  captures/delayed-1.json
d96f72618d5e4b305e891edf35ec8209b4ca4f7e218dad06461a21001cdb85db  captures/delayed-2.log
fb0c3c9afa2bd9f8ca82fa786d8e190ea13c6abc337dda248be6bc2d9704bcc9  captures/delayed-2.json
```

Primary references: [Linux source checksums](https://cdn.kernel.org/pub/linux/kernel/v6.x/sha256sums.asc),
[kernel initramfs](https://www.kernel.org/doc/html/latest/filesystems/ramfs-rootfs-initramfs.html),
[/dev/kmsg ABI](https://www.kernel.org/doc/Documentation/ABI/testing/dev-kmsg),
[serial console](https://www.kernel.org/doc/html/latest/admin-guide/serial-console.html),
[reproducible kernel builds](https://www.kernel.org/doc/html/latest/kbuild/reproducible-builds.html),
[QEMU versioned ARM virt machine](https://www.qemu.org/docs/master/system/arm/virt.html),
and [QEMU invocation](https://www.qemu.org/docs/master/system/invocation.html).
