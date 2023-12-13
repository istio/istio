// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

#include "ambient_redirect.h"

#define dbg(fmt, ...)                                   \
    ({                                                  \
        if (log_level && *log_level >= LOG_DEBUG)       \
            bpf_printk("[Debug]: " fmt, ##__VA_ARGS__); \
    })


/* This is an array to store log level
|-----------------------------------------------------------------|
|             |                 Val(log level)                    |
|-----------------------------------------------------------------|
|  0          | 0:none, 1:fatal, 2:error, 3:warn, 4:info, 5:debug |
|-----------------------------------------------------------------|
*/

struct {
        __uint(type, BPF_MAP_TYPE_ARRAY);
        __uint(max_entries, 1);
        __type(key, __u32);
        __type(value, __u32);
        __uint(pinning, LIBBPF_PIN_BY_NAME);
} log_level SEC(".maps");

/* This is an array to store host network(ip) info
|--------------------------------------------------------------|
|             |         Val(host ipv4/ipv6)                    |
|--------------------------------------------------------------|
|  0          |        ::ffff:192.0.2.1                        |
|--------------------------------------------------------------|
|  1          |        2001:db8:::1234                         |
|--------------------------------------------------------------|
*/

struct {
        __uint(type, BPF_MAP_TYPE_ARRAY);
        __uint(max_entries, 2);
        __type(key, __u32);
        __type(value, struct host_info);
        __uint(pinning, LIBBPF_PIN_BY_NAME);
} host_ip_info SEC(".maps");

/* This is an one line Array to store current ztunnel info
|--------------------------------------------------------------|
|  0          | Val(ztunnel info: ifindex, mac address)        |
|--------------------------------------------------------------|
|  0          |     6, 00:11:22:33:44:55                       |
|--------------------------------------------------------------|
*/

struct {
        __uint(type, BPF_MAP_TYPE_ARRAY);
        __uint(max_entries, 1);
        __type(key, __u32);
        __type(value, struct ztunnel_info);
        __uint(pinning, LIBBPF_PIN_BY_NAME);
} ztunnel_info SEC(".maps");

/* This is a hash map to store current application info
|--------------------------------------------------------------------------|
|  key(pod IP)         |        Val(app info: ifindex, mac address)        |
|--------------------------------------------------------------------------|
|  10.101.1.2          |        2, 00:11:22:33:44:aa                       |
|--------------------------------------------------------------------------|
|  10.101.2.2          |        3, 00:11:22:33:44:bb                       |
|--------------------------------------------------------------------------|
*/

struct {
        __uint(type, BPF_MAP_TYPE_HASH);
        __uint(max_entries, APP_INFO_MAP_SIZE);
        __type(key, __u32);
        __type(value, struct app_info);
        __uint(pinning, LIBBPF_PIN_BY_NAME);
} app_info SEC(".maps");

static __inline struct host_info * get_host_ip(int is_v6)
{
    uint32_t key = is_v6? 1 : 0;
    return bpf_map_lookup_elem(&host_ip_info, &key);
}

// ipv4 should be in big endian
static __inline struct app_info * get_app_info_from_ipv4(__u32 ipv4)
{
    return bpf_map_lookup_elem(&app_info, &ipv4);
}

static __inline struct ztunnel_info * get_ztunnel_info()
{
    uint32_t key = 0;
    return bpf_map_lookup_elem(&ztunnel_info, &key);
}

static __inline __u32 * get_log_level()
{
    uint32_t key = 0;
    return bpf_map_lookup_elem(&log_level, &key);
}

// For app pod veth pair(in host ns) ingress hook
SEC("tc")
int app_outbound(struct __sk_buff *skb)
{
    void *data = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;
    struct ethhdr *eth = data;
    struct iphdr *iph;
    struct udphdr *udph;
    struct ztunnel_info *zi = get_ztunnel_info();
    uint8_t capture_dns = 0;
    struct host_info *host_info = NULL;
    __u32 *log_level = get_log_level();

    if (!zi || zi->ifindex == 0)
        return TC_ACT_SHOT;

    capture_dns = zi->flag & CAPTURE_DNS_FLAG;

    if (data + sizeof(*eth) > data_end)
        return TC_ACT_OK;

    // TODO: support ipv6
    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return TC_ACT_OK;

    iph = data + sizeof(*eth);
    if (data + sizeof(*eth) + sizeof(*iph) > data_end)
        return TC_ACT_OK;

    host_info = get_host_ip(0);
    if (host_info && host_info->addr[3] == iph->daddr)
        return TC_ACT_OK;

    if (iph->protocol != IPPROTO_TCP && iph->protocol != IPPROTO_UDP)
        return TC_ACT_OK;

    if (!capture_dns && iph->protocol == IPPROTO_UDP) {
        if (data + sizeof(*eth) + sizeof(*iph) + sizeof(*udph) > data_end)
            return TC_ACT_OK;
        udph = data + sizeof(*eth) + sizeof(*iph);
        if (udph->dest == bpf_htons(UDP_P_DNS))
            return TC_ACT_OK;
    }

    __builtin_memcpy(eth->h_dest, zi->mac_addr, ETH_ALEN);

    skb->cb[4] = OUTBOUND_CB;

    dbg("outbound redirect to ztunnel ifidx: %u\n", zi->ifindex);
    return bpf_redirect(zi->ifindex, 0);
}

// For app pod veth pair(in host ns) egress hook
SEC("tc")
int app_inbound(struct __sk_buff *skb)
{
    void *data = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;
    struct ethhdr  *eth = data;
    struct iphdr *iph = NULL;
    struct udphdr *udph;
    struct ztunnel_info *zi = NULL;
    struct host_info *host_info = NULL;
    __u32 *log_level = get_log_level();

    if (skb->cb[4] == BYPASS_CB)
        return TC_ACT_OK;

    if (data + sizeof(*eth) > data_end)
        return TC_ACT_OK;

    // TODO: support ipv6
    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return TC_ACT_OK;

    iph = data + sizeof(*eth);
    if (data + sizeof(*eth) + sizeof(*iph) > data_end)
        return TC_ACT_OK;

    host_info = get_host_ip(0);
    if (host_info && host_info->addr[3] == iph->saddr)
        return TC_ACT_OK;

    if (iph->protocol != IPPROTO_TCP && iph->protocol != IPPROTO_UDP)
        return TC_ACT_OK;

    zi = get_ztunnel_info();

    if (!zi || zi->ifindex == 0) {
        dbg("inbound_to_ztunnel unable to retrieve ztunnel_info\n");
        return TC_ACT_SHOT;
    }

    if (iph->protocol == IPPROTO_UDP) {
        if (data + sizeof(*eth) + sizeof(*iph) + sizeof(*udph) > data_end)
            return TC_ACT_OK;
        udph = data + sizeof(*eth) + sizeof(*iph);
        if (udph->source == bpf_htons(UDP_P_DNS))
            return TC_ACT_OK;
    }

    __builtin_memcpy(eth->h_dest, zi->mac_addr, ETH_ALEN);

    skb->cb[4] = INBOUND_CB;

    dbg("inbound redirect to ztunnel ifidx: %u\n", zi->ifindex);
    return bpf_redirect(zi->ifindex, 0);
}

// For ztunnel pod veth pair(in host ns) ingress hook
SEC("tc")
int ztunnel_host_ingress(struct __sk_buff *skb)
{
    void *data = (void *)(long)skb->data;
    struct ethhdr  *eth = data;
    void *data_end = (void *)(long)skb->data_end;
    struct iphdr *iph = NULL;
    struct app_info *pi = NULL;
    __u32 *log_level = get_log_level();

    if (data + sizeof(*eth) > data_end)
        return TC_ACT_OK;

    // TODO: support ipv6
    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return TC_ACT_OK;

    iph = data + sizeof(*eth);
    if (data + sizeof(*eth) + sizeof(*iph) > data_end)
        return TC_ACT_OK;

    if (iph->protocol != IPPROTO_TCP && iph->protocol != IPPROTO_UDP)
        return TC_ACT_OK;

    pi = get_app_info_from_ipv4(iph->daddr);
    if (!pi) {
        // ip is not in ambient managed mesh
        // dbg("ztunnel_host_ingress unable to retrieve pod info: 0x%x\n", bpf_ntohl(iph->daddr));
        return TC_ACT_OK;
    }

    __builtin_memcpy(eth->h_dest, pi->mac_addr, ETH_ALEN);
    skb->cb[4] = BYPASS_CB;

    dbg("ztunnel redirect to app(0x%x) ifidx: %u\n", bpf_ntohl(iph->daddr), pi->ifindex);

    return bpf_redirect(pi->ifindex, 0);
}

// For ztunnel pod veth pair(in pod ns) ingress hook
SEC("tc")
int ztunnel_ingress(struct __sk_buff *skb)
{
    void *data = (void *)(long)skb->data;
    struct ethhdr  *eth = data;
    void *data_end = (void *)(long)skb->data_end;
    struct bpf_sock_tuple *tuple;
    size_t tuple_len;
    struct bpf_sock *sk;
    int skip_mark = 0;
    __u32 *log_level = get_log_level();

    if (skb->cb[4] != OUTBOUND_CB && skb->cb[4] != INBOUND_CB)
        return TC_ACT_OK;

    if (data + sizeof(*eth) > data_end)
        return TC_ACT_OK;

    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return TC_ACT_OK;

    struct iphdr *iph = data + sizeof(*eth);
    if (data + sizeof(*eth) + sizeof(*iph) > data_end)
        return TC_ACT_OK;

    // TODO: support UDP tunneling
    if (iph->protocol != IPPROTO_TCP)
        return TC_ACT_OK;

    tuple = (struct bpf_sock_tuple *)&iph->saddr;
    tuple_len = sizeof(tuple->ipv4);
    if ((void *)tuple + sizeof(tuple->ipv4) > (void *)(long)skb->data_end)
        return TC_ACT_SHOT;

    if (skb->cb[4] == OUTBOUND_CB) {
        // We mark all app egress pkt as outbound and redirect here.
        // We need identify if it's a actual *outbound* or just a response to
        // the proxy
        sk = bpf_skc_lookup_tcp(skb, tuple, tuple_len, BPF_F_CURRENT_NETNS, 0);
        if (sk) {
            if (sk->state != BPF_TCP_LISTEN) {
                skip_mark = 1;
            }
            bpf_sk_release(sk);
        }
        if (!skip_mark) {
            skb->mark = ZTUNNEL_OUTBOUND_MARK;
            dbg("got packet with out cb mark 0x%x, mark packet: %u\n", skb->cb[4], skb->mark);
        }
    } else if (skb->cb[4] == INBOUND_CB) {
        // For original source case, app will overwrite
        // the cb[4] and redirect here.
        sk = bpf_skc_lookup_tcp(skb, tuple, tuple_len, BPF_F_CURRENT_NETNS, 0);
        if (sk) {
            if (sk->state != BPF_TCP_LISTEN
                && sk->src_port != ZTUNNEL_INBOUND_PORT
                && sk->src_port != ZTUNNEL_INBOUND_PLAINTEXT_PORT) {
                skip_mark = 1;
            }
            bpf_sk_release(sk);
        }
        if (!skip_mark) {
            skb->mark = ZTUNNEL_INBOUND_MARK;
            dbg("got inbound packet with in cb 0x%x, mark packet: %u\n", skb->cb[4], skb->mark);
        }
    }

    return TC_ACT_OK;
}

// For ztunnel pod veth pair(in pod ns) ingress hook
// prog name disp limits to 15 bytes
SEC("tc")
int ztunnel_tproxy(struct __sk_buff *skb)
{
    void *data = (void *)(long)skb->data;
    struct ethhdr  *eth = data;
    void *data_end = (void *)(long)skb->data_end;
    struct bpf_sock_tuple *tuple;
    size_t tuple_len;
    struct bpf_sock *sk;
    __u32 *log_level = get_log_level();
    struct bpf_sock_tuple proxy_tup;
    __be16 proxy_port;
    int ret;

    if (skb->cb[4] != OUTBOUND_CB && skb->cb[4] != INBOUND_CB)
        return TC_ACT_OK;

    if (data + sizeof(*eth) > data_end)
        return TC_ACT_OK;

    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return TC_ACT_OK;

    struct iphdr *iph = data + sizeof(*eth);
    if (data + sizeof(*eth) + sizeof(*iph) > data_end)
        return TC_ACT_OK;

    // TODO: support UDP tunneling
    if (iph->protocol != IPPROTO_TCP)
        return TC_ACT_OK;

    tuple = (struct bpf_sock_tuple *)&iph->saddr;
    tuple_len = sizeof(tuple->ipv4);
    if ((void *)tuple + sizeof(tuple->ipv4) > (void *)(long)skb->data_end)
        return TC_ACT_SHOT;

    sk = bpf_skc_lookup_tcp(skb, tuple, tuple_len, BPF_F_CURRENT_NETNS, 0);
    if (sk && sk->state == BPF_TCP_LISTEN) {
        bpf_sk_release(sk);
        sk = NULL;
    }
    if (!sk) {
        // No existing connection, try to find listner
        __builtin_memset(&proxy_tup, 0, sizeof(proxy_tup));

        if (skb->cb[4] == OUTBOUND_CB) {
            proxy_port = bpf_htons(ZTUNNEL_OUTBOUND_PORT);
        } else {
            if (tuple->ipv4.dport != bpf_htons(ZTUNNEL_INBOUND_PORT)) {
                // for plaintext case
                proxy_port = bpf_htons(ZTUNNEL_INBOUND_PLAINTEXT_PORT);
            } else {
                proxy_port = bpf_htons(ZTUNNEL_INBOUND_PORT);
            }
        }

        proxy_tup.ipv4.dport = proxy_port;

        sk = bpf_skc_lookup_tcp(skb, &proxy_tup, tuple_len, BPF_F_CURRENT_NETNS, 0);
        if (sk && sk->state != BPF_TCP_LISTEN) {
            bpf_sk_release(sk);
            sk = NULL;
        }
    }
    if (!sk) {
        return TC_ACT_OK;
    }

    ret = bpf_sk_assign(skb, sk, 0);
    bpf_sk_release(sk);

    skb->mark = ZTUNNEL_TPROXY_MARK;
    return ret == 0 ? TC_ACT_OK : TC_ACT_SHOT;
}

char __license[] SEC("license") = "Dual BSD/GPL";
