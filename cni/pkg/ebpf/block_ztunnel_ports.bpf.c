// clang-format off
#include "vmlinux.h"
// clang-format on

#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

#define AF_INET 2
#define AF_INET6 10

volatile const __u32 ztunnel_mark = 1337;
volatile const __u32 ztunnel_mask = 0xfff;
volatile const __u16 ztunnel_outbound_port = 15001;
volatile const __u16 ztunnel_inbound_port = 15008;
volatile const __u16 ztunnel_inbound_plain_port = 15006;
volatile const __u16 ztunnel_dns_port = 15053;

#define MAX_ENTRIES 1024

// If we want to support kernels that doesn't have LRU_HASH, we should implement
// a user mode component that GC's the map. otherwise, as long as (MAX_ENTRIES >
// max_pods_per_node), we should be fine.
struct {
  __uint(type, BPF_MAP_TYPE_LRU_HASH);
  __type(key, u64);  // cgroup id
  __type(value,
         bool);  // should enforce that only ztunnel binds to ztunnel ports
  __uint(max_entries, MAX_ENTRIES);
  __uint(pinning, LIBBPF_PIN_BY_NAME);

} should_enforce_bind SEC(".maps");

// The idea here is to rachet the binding of sockets - if in a certain cgroup
// the ztunnel binds to its ports, then from that moment on, only ztunnel will
// be allowed to bind to those ports.
static __always_inline int bind_prog(struct bpf_sock_addr *ctx, int family) {
  struct bpf_sock *sk;
  __u32 port;
  __u32 mark;
  bool should_enforce;
  bool is_ztunnel;
  bool *should_enforce_p;

  sk = ctx->sk;
  if (!sk) return 1;

  if (sk->family != family) return 1;

  port = bpf_ntohs(ctx->user_port);
  if (ctx->type == SOCK_STREAM) {
    if ((port != ztunnel_outbound_port) && (port != ztunnel_inbound_port) &&
        (port != ztunnel_inbound_plain_port)) {
      return 1;
    }
  } else if (ctx->type == SOCK_DGRAM) {
    if (port != ztunnel_dns_port) {
      return 1;
    }
  }

  is_ztunnel = (sk->mark & ztunnel_mask) == ztunnel_mark;
  should_enforce = false;

  u64 cgroup_id = bpf_get_current_cgroup_id();
  should_enforce_p = bpf_map_lookup_elem(&should_enforce_bind, &cgroup_id);

  if (is_ztunnel && (should_enforce_p == NULL || (!*should_enforce_p))) {
    // ztunnel binds, save it so we enforce it next time
    bpf_map_update_elem(&should_enforce_bind, &cgroup_id, &is_ztunnel, BPF_ANY);
  }

  if (should_enforce_p) {
    should_enforce = *should_enforce_p;
  }

  if (should_enforce && !is_ztunnel) {
    return 0;
  }
  return 1;
}

SEC("cgroup/bind4")
int bind_v4_prog(struct bpf_sock_addr *ctx) { return bind_prog(ctx, AF_INET); }

SEC("cgroup/bind6")
int bind_v6_prog(struct bpf_sock_addr *ctx) { return bind_prog(ctx, AF_INET6); }

// char _license[] SEC("license") = "GPL";
