#!/bin/sh
set -eu
export LC_ALL=C TZ=UTC
export KBUILD_BUILD_TIMESTAMP='2026-11-15 00:00:00 +0000'
export KBUILD_BUILD_USER=bootlog KBUILD_BUILD_HOST=example KBUILD_BUILD_VERSION=1
mkdir /work
tar -xJf /tmp/linux-6.12.60.tar.xz -C /work
cd /work/linux-6.12.60
cp /tmp/kernel.config .config
make ARCH=arm64 olddefconfig > /work/build.log 2>&1
cmp /tmp/kernel.config .config
if ! make -j2 ARCH=arm64 Image >> /work/build.log 2>&1; then
    tail -n 60 /work/build.log
    exit 1
fi
gcc -std=c11 -Os -s -Wall -Wextra -Werror -static -Wl,--build-id=none \
    -o /work/init /tmp/init.c
gcc -O2 -o /work/gen_init_cpio usr/gen_init_cpio.c
touch -t 202611150000.00 /work/init
/work/gen_init_cpio -t 1794700800 /tmp/initramfs.list > /work/initramfs.cpio
cp arch/arm64/boot/Image /work/Image
cp .config /work/kernel.config
qemu-system-aarch64 --version
gcc --version | head -n 1
ld --version | head -n 1
sha256sum /work/Image /work/initramfs.cpio /work/kernel.config
