#define _POSIX_C_SOURCE 200809L
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/reboot.h>
#include <time.h>
#include <unistd.h>

static int kmsg = -1;

static void fail(const char *operation) {
    dprintf(kmsg >= 0 ? kmsg : STDERR_FILENO,
            "<3>BOOTLOG_GUEST_ERROR %s\n", operation);
    reboot(RB_POWER_OFF);
    for (;;) pause();
}

static void marker(const char *message) {
    char line[128];
    int n = snprintf(line, sizeof(line), "<6>%s\n", message);
    if (n < 0 || (size_t)n >= sizeof(line) || write(kmsg, line, (size_t)n) != n)
        fail("write kmsg");
}

static void delay(long seconds, long nanoseconds) {
    struct timespec remaining = {seconds, nanoseconds};
    while (nanosleep(&remaining, &remaining) < 0)
        if (errno != EINTR) fail("nanosleep");
}

int main(void) {
    kmsg = open("/dev/kmsg", O_WRONLY);
    if (kmsg < 0) fail("open kmsg");
    if (mount("proc", "/proc", "proc", 0, NULL) < 0) fail("mount proc");
    int fd = open("/proc/cmdline", O_RDONLY);
    if (fd < 0) fail("open cmdline");
    char command[2048];
    ssize_t n = read(fd, command, sizeof(command) - 1);
    if (n <= 0 || n == (ssize_t)sizeof(command) - 1) fail("read cmdline");
    command[n] = '\0';
    close(fd);
    const char *scenario = NULL;
    for (char *part = strtok(command, " \n"); part; part = strtok(NULL, " \n")) {
        if (strncmp(part, "bootlog.scenario=", 17) == 0) {
            if (scenario) fail("duplicate scenario");
            scenario = part + 17;
        }
    }
    if (!scenario || (strcmp(scenario, "normal") && strcmp(scenario, "missing") &&
                      strcmp(scenario, "forbidden") && strcmp(scenario, "delayed") &&
                      strcmp(scenario, "hang"))) fail("invalid scenario");
    char message[64];
    snprintf(message, sizeof(message), "BOOTLOG_SCENARIO %s", scenario);
    marker(message);
    if (!strcmp(scenario, "hang")) for (;;) pause();
    delay(!strcmp(scenario, "delayed") ? 10 : 1, 0);
    if (strcmp(scenario, "missing")) marker("BOOTLOG_READY");
    if (!strcmp(scenario, "forbidden")) {
        delay(1, 0);
        marker("BOOTLOG_FORBIDDEN");
    }
    marker("BOOTLOG_COMPLETE");
    delay(0, 100000000);
    if (reboot(RB_POWER_OFF) < 0) fail("poweroff");
    for (;;) pause();
}
