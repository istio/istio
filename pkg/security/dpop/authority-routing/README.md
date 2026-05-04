# Fix: Dynamic :authority routing — istio/istio#59669

Routes ingress traffic to the correct upstream Kubernetes service when the
`:authority` header contains only a short name like `my-service`, with no
static VirtualService per service.

---

## Root cause

Envoy's `cluster_header` does an exact lookup against registered cluster names.
Istio registers clusters as:

```
outbound|<port>|<subset>|<fqdn>
# e.g.  outbound|80||my-service.default.svc.cluster.local
```

A short name like `my-service` never matches, so `cluster_header` alone fails.
VirtualService destinations must be statically declared — no runtime header
interpolation exists in Istio's control plane.

---

## Solution

Two EnvoyFilters + one Gateway/VirtualService pair. No per-service config needed.

```
Client: :authority = "my-service"
              │
   ┌──────────▼──────────────────────────────┐
   │  Lua HTTP filter  (GATEWAY context)      │
   │                                          │
   │  :authority  →  my-service.default.      │
   │                 svc.cluster.local        │
   │                                          │
   │  x-envoy-upstream-rq-cluster  →          │
   │    outbound|80||my-service.default.      │
   │    svc.cluster.local                     │
   └──────────┬──────────────────────────────┘
              │
   ┌──────────▼──────────────────────────────┐
   │  cluster_header route  (EnvoyFilter      │
   │  REPLACE on HTTP_ROUTE)                  │
   │                                          │
   │  Reads x-envoy-upstream-rq-cluster →     │
   │  routes to exact Envoy cluster           │
   │  Strips header before upstream           │
   └──────────┬──────────────────────────────┘
              │
   upstream: my-service.default.svc.cluster.local:80
```

---

## Files

| File | Purpose |
|------|---------|
| `envoyfilter-lua.yaml` | Lua filter on GATEWAY — rewrites `:authority` + sets cluster header |
| `envoyfilter-cluster-header.yaml` | REPLACE route action with `cluster_header` routing |
| `gateway.yaml` | Gateway (port 80, `*`) + catch-all VS + placeholder ServiceEntry |
| `wasm-plugin.yaml` | WasmPlugin CR (production alternative to Lua) |
| `wasm/main.go` | Go/TinyGo source for the WASM plugin |
| `test.sh` | End-to-end test script |

---

## Deploy

```bash
# 1. Edit DEFAULT_NAMESPACE in envoyfilter-lua.yaml
kubectl apply -f gateway.yaml
kubectl apply -f envoyfilter-lua.yaml
kubectl apply -f envoyfilter-cluster-header.yaml
```

## Test

```bash
INGRESS=$(kubectl -n istio-system get svc istio-ingressgateway \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# Short name → routed to my-service.default.svc.cluster.local
curl -H "Host: my-service" http://$INGRESS/

# With port
curl -H "Host: my-service:8080" http://$INGRESS/

# Cross-namespace via hint header (stripped before upstream)
curl -H "Host: my-service" -H "x-target-namespace: payments" http://$INGRESS/

# FQDN passthrough — Lua filter skips names containing dots
curl -H "Host: my-service.payments.svc.cluster.local" http://$INGRESS/
```

---

## Key implementation details

**Cluster name format** — confirmed from `pilot/pkg/model/service.go BuildSubsetKey`:
```go
func BuildSubsetKey(direction, subsetName, hostname, port) string {
    return direction + "|" + port + "|" + subsetName + "|" + hostname
    // → "outbound|80||my-service.default.svc.cluster.local"
}
```

**Vhost name format** — confirmed from `pilot/pkg/networking/util DomainName()`:
```go
func DomainName(host string, port int) string { return net.JoinHostPort(host, port) }
// Gateway with hosts: ["*"] on port 80 → vhost name = "*:80"
```

**Why REPLACE not MERGE for the route** — REPLACE is explicit and avoids any
interaction with the existing `cluster` field in the route action oneof.
MERGE would also work (Istio's `merge.Merge` handles oneof replacement via
`src.Range` + `dst.Set`), but REPLACE is clearer in intent.

**Why a ServiceEntry placeholder** — Istio's VirtualService validation rejects
destinations that don't exist in the service registry. The ServiceEntry
`dynamic-routing.istio-system.svc.cluster.local` satisfies validation; the
`cluster_header` EnvoyFilter replaces the cluster specifier at xDS time so
traffic never actually reaches the placeholder endpoint.

**WasmPlugin phase** — use `UNSPECIFIED_PHASE` (default) on the gateway.
`AUTHN` is for sidecar JWT/mTLS processing and is incorrect here.

---

## Approach comparison

| | Lua EnvoyFilter | WASM Plugin |
|---|---|---|
| Build step | None | TinyGo + OCI push |
| Overhead | ~1µs/req | ~1µs/req |
| Unit testable | No | Yes (Go) |
| Istio version | 1.5+ | 1.12+ |
| Recommended for | Quick deployment | Teams with CI/CD |
