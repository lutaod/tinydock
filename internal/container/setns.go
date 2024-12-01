// This file implements container exec command which allows running commands
// inside a container's namespaces. Due to Linux kernel restrictions on mount
// namespace operations in multi-threaded processes, a C constructor is used
// to enter namespaces before Go runtime spins up additional threads.

package container

/*
#define _GNU_SOURCE
#include <errno.h>
#include <sched.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <fcntl.h>
#include <unistd.h>

#define MAX_PATH 1024

__attribute__((constructor)) void enter_namespace(void) {
   const char* container_pid = getenv("TINYDOCK_PID");
   const char* container_cmd = getenv("TINYDOCK_CMD");

   if (!container_pid || !container_cmd) {
       return;
   }

   char nspath[MAX_PATH];
   const char* namespaces[] = { "ipc", "uts", "net", "pid", "mnt" };

   for (int i = 0; i < sizeof(namespaces) / sizeof(namespaces[0]); i++) {
       if (snprintf(nspath, sizeof(nspath), "/proc/%s/ns/%s",
                   container_pid, namespaces[i]) >= sizeof(nspath)) {
           fprintf(stderr, "path too long for namespace %s\n", namespaces[i]);
           exit(1);
       }

       int fd = open(nspath, O_RDONLY);
       if (fd < 0) {
           fprintf(stderr, "failed to open %s namespace: %s\n",
                   namespaces[i], strerror(errno));
           exit(1);
       }

       if (setns(fd, 0) == -1) {
           fprintf(stderr, "failed to enter %s namespace: %s\n",
                   namespaces[i], strerror(errno));
           close(fd);
           exit(1);
       }
       close(fd);
   }

   if (system(container_cmd) == -1) {
       fprintf(stderr, "failed to execute command: %s\n", strerror(errno));
       exit(1);
   }

   exit(0);
}
*/
import "C"
