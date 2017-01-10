/*
** server.c
 * Demo server to check that we can extract all parameters from an incoming
*request
 * and respond to it
*/

#include <arpa/inet.h>
#include <errno.h>
#include <linux/netfilter_ipv4.h>
#include <netdb.h>
#include <netinet/in.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

#define DEFAULT_PORT \
  "3490"  // the port users will be connecting to, if not specified on command
          // line

#define QUEUE_SIZE 10  // pending connections queue size

void sigchld_handler(int s) {
  int saved_errno = errno;

  while (waitpid(-1, NULL, WNOHANG) > 0)
    ;

  errno = saved_errno;
}

int main(int argc, char *argv[]) {
  struct sigaction sa;
  const int yes = 1;
  char *port = DEFAULT_PORT;
  int rv;

  if (argc > 1) {
    port = argv[1];
  }

  struct addrinfo hints;
  memset(&hints, 0, sizeof hints);
  hints.ai_family = AF_INET;
  hints.ai_socktype = SOCK_STREAM;
  hints.ai_flags = AI_PASSIVE;  // use my IP

  struct addrinfo *server_info = NULL;
  if ((rv = getaddrinfo(NULL, port, &hints, &server_info)) != 0) {
    fprintf(stderr, "getaddrinfo: %s\n", gai_strerror(rv));
    return 1;
  }

  struct addrinfo *p;
  int sockfd;  // listen on sock_fd
  for (p = server_info; p != NULL; p = p->ai_next) {
    if ((sockfd = socket(p->ai_family, p->ai_socktype, p->ai_protocol)) == -1) {
      perror("server: socket");
      continue;
    }

    if (setsockopt(sockfd, SOL_SOCKET, SO_REUSEADDR, &yes, sizeof(int)) == -1) {
      perror("setsockopt");
      exit(1);
    }

    if (bind(sockfd, p->ai_addr, p->ai_addrlen) == -1) {
      close(sockfd);
      perror("server: bind");
      continue;
    }

    break;
  }

  freeaddrinfo(server_info);

  if (p == NULL) {
    fprintf(stderr, "server: failed to bind\n");
    exit(1);
  }
  if (listen(sockfd, QUEUE_SIZE) == -1) {
    perror("listen");
    exit(1);
  }

  sa.sa_handler = sigchld_handler;
  sigemptyset(&sa.sa_mask);
  sa.sa_flags = SA_RESTART;
  if (sigaction(SIGCHLD, &sa, NULL) == -1) {
    perror("sigaction");
    exit(1);
  }

  char bind_addr_str[INET6_ADDRSTRLEN] = {0};
  struct sockaddr_in *bind_sock_addr = (struct sockaddr_in *)p->ai_addr;
  inet_ntop(p->ai_family, &bind_sock_addr->sin_addr, bind_addr_str,
            p->ai_addrlen);
  printf("server %s: waiting for connections on port %s:%u...\n", port,
         bind_addr_str, ntohs(bind_sock_addr->sin_port));

  while (1) {
    socklen_t addr_len = sizeof(struct sockaddr_storage);
    int addr_str_len = INET6_ADDRSTRLEN;

    struct sockaddr_storage their_addr = {
        0};  // connector's address information
    int accepted_fd = accept(sockfd, (struct sockaddr *)&their_addr, &addr_len);
    if (accepted_fd == -1) {
      perror("accept");
      continue;
    }

    struct sockaddr_in *src_sock_addr = (struct sockaddr_in *)&their_addr;
    char their_addr_str[INET6_ADDRSTRLEN] = {0};
    inet_ntop(their_addr.ss_family, &src_sock_addr->sin_addr, their_addr_str,
              addr_str_len);
    printf("server %s: got connection FROM %s:%u\n", port, their_addr_str,
           ntohs(src_sock_addr->sin_port));

    struct sockaddr_storage my_addr = {0};  // my address information
    struct sockaddr_in *dst_sock_addr = (struct sockaddr_in *)&my_addr;
    char my_addr_str[INET6_ADDRSTRLEN] = {0};
    getsockname(accepted_fd, (struct sockaddr *)dst_sock_addr, &addr_len);
    inet_ntop(my_addr.ss_family, &dst_sock_addr->sin_addr, my_addr_str,
              addr_str_len);
    printf("server %s: got connection TO %s:%u\n", port, my_addr_str,
           ntohs(dst_sock_addr->sin_port));

    struct sockaddr_storage orig_addr = {0};  // orig address information
    struct sockaddr_in *orig_sock_addr = (struct sockaddr_in *)&orig_addr;
    char orig_addr_str[INET6_ADDRSTRLEN] = {0};
    int status = getsockopt(accepted_fd, SOL_IP, SO_ORIGINAL_DST,
                            orig_sock_addr, &addr_len);

    if (status == 0) {
      inet_ntop(orig_addr.ss_family, &orig_sock_addr->sin_addr, orig_addr_str,
                addr_str_len);
      printf("server %s: ORIG DEST %s:%u\n", port, orig_addr_str,
             ntohs(orig_sock_addr->sin_port));
    } else {
      printf("Could not get orig destination from accepted socket.\n");
    }

    if (!fork()) {  // this is the child process

      close(sockfd);
      char msg[256] = {0};
      snprintf(msg, 256, "FROM %s:%u, TO %s:%u, ORIG DEST %s:%u\n",
               their_addr_str, ntohs(src_sock_addr->sin_port), my_addr_str,
               ntohs(dst_sock_addr->sin_port), orig_addr_str,
               ntohs(orig_sock_addr->sin_port));

      if (send(accepted_fd, msg, strlen(msg), 0) == -1) {
        perror("send");
      }
      close(accepted_fd);
      exit(0);
    }
    close(accepted_fd);
  }

  return 0;
}
