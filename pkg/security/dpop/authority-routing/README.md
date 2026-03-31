# Fix: Dynamic :authority routing (istio/istio#59669)

Dynamically routes ingress traffic to the correct upstream Kubernetes service
when the `:authority` header contains only a short name like `my-service`,
without any static VirtualService per service.

---

## Root cause

Envoy's `cluster_header` does an **exact** lookup against registered cluster
names. Istio registers clusters as:

```
outbound|<port>|<subset>|<fqdn>
# e.g. outbound|80||my-service.default.svc.cluster.local
```

A short name like `my-service` never matches, so `cluster_header` alone fails.
VirtualService destinations must be statically declared — no runtime header
interpolation exists.

---

## Solution: Lua filter + cluster_header (no static VS per service)

```
Client: :authority = "my-service"
              │
   ┌──────────▼──────────┐
   │  Lua HTTP filter     │  (EnvoyFilter on GATEWAY)
   │  1. Rewrites         │
   │     :authority →     │
   │     my-service.      │
   │     default.svc.     │
   │     cluster.local    │
   │  2. Sets header      │
   │     x-envoy-         │
   │     upstream-rq-     │
   │     cluster =        │
   │     outbound|80||    │
   │     my-service...    │
   └──────────┬──────────┘
              │
   ┌──────────▼──────────┐
   │  cluster_header      │  (EnvoyFilter MERGE on VIRTUAL_HOST/HTTP_ROUTE)
   │  reads header →      │
   │  routes to exact     │
   │  Envoy cluster       │
   └──────────┬──────────┘
              │
   upstream: my-service.default.svc.cluster.local:80
```

---

## Files

| File | Purpose |
|------|---------|
| `envoyfilter-lua.yaml` | Lua filter — rewrites `:authority` + sets cluster header |
| `envoyfilter-cluster-header.yaml` | Enables `cluster_header` routing on the gateway vhost |
| `gateway.yaml` | Gateway (port 80, wildcard host) + single catch-all VirtualService |
| `wasm-plugin.yaml` | WasmPlugin CR (production alternative to Lua) |
| `wasm/main.go` | Go/TinyGo source for the WASM plugin |
| `test.sh` | End-to-end test script |

---

## Quick start (Lua — no build needed)

```bash
# 1. Edit DEFAULT_NAMESPACE in envoyfilter-lua.yaml
# 2. Apply all configs
kubectl apply -f gateway.yaml
kubectl apply -f envoyfilter-lua.yaml
kubectl apply -f envoyfilter-cluster-header.yaml

# 3. Test — send a request with a short :authority header
INGRESS_IP=$(kubectl -n istio-system get svc istio-ingressgateway \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

curl -v -H "Host: my-service" http://$INGRESS_IP/
# :authority is rewritten to my-service.default.svc.cluster.local
# routed to outbound|80||my-service.default.svc.cluster.local
```

---

## Multi-namespace support

Send a hint header from the client:

```bash
curl -H "Host: my-service" \
     -H "x-target-namespace: payments" \
     http://$INGRESS_IP/
# routes to my-service.payments.svc.cluster.local
```

The Lua filter strips `x-target-namespace` before forwarding upstream.

---

## WASM plugin (production alternative)

```bash
# Build
cd wasm
tinygo build -o authority-rewrite.wasm -scheduler=none -target=wasi ./main.go

# Push to OCI registry
crane push authority-rewrite.wasm your-registry/authority-rewrite:v1.0.0

# Edit wasm-plugin.yaml with your registry URL, then:
kubectl apply -f wasm-plugin.yaml
kubectl apply -f envoyfilter-cluster-header.yaml
kubectl apply -f gateway.yaml
```

---

## Approach comparison

| | Lua EnvoyFilter | WASM Plugin |
|---|---|---|
| Build step | None | TinyGo required |
| Performance | ~1µs/req | ~1µs/req |
| Testability | Manual | Unit-testable in Go |
| Istio version | 1.5+ | 1.12+ |
| Production ready | Yes | Yes (preferred for teams with CI/CD) |

---

## Key implementation notes

- Cluster name format is defined in `pilot/pkg/model/service.go BuildSubsetKey`:
  `direction|port|subset|hostname` → `outbound|80||svc.ns.svc.cluster.local`
- `x-envoy-upstream-rq-cluster` is the correct header for dynamic cluster
  selection — it is consumed by Envoy internally and must be stripped from
  the upstream request (handled by the `request_headers_to_remove` in
  `envoyfilter-cluster-header.yaml`)
- No VirtualService per service is needed — Istio auto-generates a cluster
  for every Kubernetes Service it discovers
