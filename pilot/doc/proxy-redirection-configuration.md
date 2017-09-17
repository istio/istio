# Sidecar proxy traffic redirection methods

See [Microservice Proxies in Kubernetes](https://docs.google.com/document/d/1v870Igrj5QS52G9O43fhxbV_S3mpvf_H6Hb8r85KZLY) for a detailed comparison of service-mesh proxy deployments and the initial design and implementation proposal. 

This document describes the current method and alternatives considered for redirecting inbound and outbound traffic to the per-pod sidecar proxy.

## Requirements
 * Transparency balanced with platform independence and system stability
 * Loopback bypass for local proxy aware apps (e.g. access L7 service via localhost instead of VIP)
 * Security (avoid granting unnecessary privileges to proxy process)
 
An attacker impact is minimized if envoy is compromised with non-elevated privileges. If we add any privileges to envoy container, it would raise lots of eyebrows. With privilege escalation, feature additions to envoy come under a lot of scrutiny (e.g. regex support in URLs). Elevated privileges should be limited to one-time initialization (via init-container) and kept out of the proxy container.

 * Special treatment for kube-system namespace (e.g. fluentd, ingress controllers)
 TODO
 
## Configuration for redirection inbound and outbound traffic to proxy

The following assumes all inbound and outbound traffic is redirected (via iptables) to a single port. The envoy process binds to this port and uses SO_ORIGINAL_DST to recover the original destination and pass on to the matching filter (see 
[config-listeners](https://envoyproxy.github.io/envoy/configuration/listeners/listeners.html)).
```
ISTIO_PROXY_PORT=5001
```

UID of envoy proxy process. Used for UID-based iptables redirection. 
```  
ISTIO_PROXY_UID=1337
```

Packet mark for mark-based iptables redirection.
```  
ISTIO_SO_MARK=0x1
```

Network classifier ID for net_cls-based iptables redirection.

```
ISTIO_NET_CLS_ID=0x100001
```

### Inbound 

The iptable rule for inbound redirection is straightforward assuming *all* traffic needs to be redirected to the proxy. Additional rules are needed if inbound traffic needs to bypass the proxy, e.g. ssh.

```
iptables -t nat -A PREROUTING -p tcp -j REDIRECT --to-port ${ISTIO_PROXY_PORT}
```

### Outbound 

The UID method is the current plan-of-record primarily because no elevated privileges are required in the proxy container. The downside of this method is the required coordination of UID, but in the short term this is not an issue and in the long term mitigation measures can be explored, e.g. expose overridable UID to end-user.

#### UID (--uid-owner)

```
iptables -t nat -A OUTPUT -p tcp -j REDIRECT ! -s 127.0.0.1/32 \
  --to-port ${ISTIO_PROXY_PORT} -m owner '!' --uid-owner ${ISTIO_PROXY_UID}
```     

- pro: No envoy change required
- con: Requires coordinating UID between proxy and init-container. Istio may not necessarily have control over UID (e.g. set by docker). The UID could be made configurable / overridable so the proxy UID does not conflict with other end-user UID and processes. See (Security Context)[https://kubernetes.io/docs/user-guide/security-context]. 

## Alternatives considered

#### SO_MARK packet marking (see http://man7.org/linux/man-pages/man7/socket.7.html)

```
iptables -t nat -A OUTPUT -p tcp -j REDIRECT ! -s 127.0.0.1/32 \ 
  --to-port ${ISTIO_PROXY_PORT} -m mark '!' --mark ${ISTIO_SO_MARK}
```  

- pro - ${ISTIO_SO_MARK} value can be configured in pod spec (e.g. configmap, envvar) and used by init-container to program iptables and proxy agent to create envoy config with proper SO_MARK values per-upstream cluster.
- con: Requires adding SO_MARK support to envoy, perhaps configured per upstream cluster?
- con: Requires proxy run with NET_CAP_ADMIN to set SO_MARK on sockets.

#### Network classifier cgroup (see https://www.kernel.org/doc/Documentation/cgroup-v1/net_cls.txt)

```
iptables -t nat -A OUTPUT -p tcp -j REDIRECT ! -s 127.0.0.1/32 \
  --to-port ${ISTIO_PROXY_PORT} -m cgroups '!' --cgroup ${ISTIO_NET_CLS_ID}
```

- pro: No envoy change required
- con: Requires newer version of iptables (e.g. 1.6.0)
- con: Requires exposing /sys/fs/cgroup/net_cls to proxy container so that proxy agent can add proxy PID to net_cls groups, e.g. bind-mount /sys/fs/cgroup/net_cls via pod spec. This requires additional privileges in the proxy container through the proxy agent could possibly drop privileges before fork/exec'ing envoy. Alternatively, a node-level agent could manage net_cls on behalf of per-pod proxy agent via something like UDS. 
- con: Proxy agent needs to update /sys/fs/group/net_cls/<proxy-group>/tasks file with envoy proxy PID whenever proxy crashes, restarts, etc.

I wasn't able to get this method to work. Suggestions / corrections welcome.

#### explicit rules per service

- pro: very explicit.
- con: Lots of iptable rules to cover all client and server services. Concerns around system destabilization due to large number of rules.
