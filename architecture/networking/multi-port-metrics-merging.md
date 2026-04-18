# Design Proposal: Multi-Port Metrics Merging

**Issue:** <https://github.com/istio/istio/issues/59567>

---

## Problem Statement

`handleStats` (`pilot/cmd/pilot-agent/status/server.go`) merges application metrics by scraping a single endpoint encoded in `ISTIO_PROMETHEUS_ANNOTATIONS` — a JSON blob carrying one `port` and one `path`. This works for pods with a single metrics-emitting container, but breaks for common multi-container patterns: a primary app on `:8080/metrics` paired with a sidecar exporter (e.g., node-exporter, JMX exporter, custom business metrics) on `:9100/metrics`. Under `STRICT` mTLS, Prometheus cannot reach application ports directly; all scraping must flow through the agent at `:15020/stats/prometheus`. Since the agent only fans out to one app endpoint, the second container's metrics are silently dropped with no error surfaced. The annotation rewrite in `applyPrometheusMerge` also overwrites `prometheus.io/port` to `15020`, so there is no way for the second container to be scraped at all once a sidecar is injected.

---

## Proposed Annotation Format

### New annotation: `prometheus.istio.io/scrape-targets`

```yaml
annotations:
  prometheus.istio.io/scrape-targets: "8080:/metrics,9100:/metrics"
```

Value is a comma-separated list of `port:path` pairs. Each entry is trimmed; order is preserved (determines merge order). An empty path component defaults to `/metrics`.

**Why not extend `prometheus.io/port`?**
`prometheus.io/port` is consumed by external tooling (kube-state-metrics, Prometheus operator CRDs, annotation-based scrape configs). Changing its semantics (e.g., comma-separated) would silently break any cluster that scrapes it without the Istio agent in the path. Non-starter.

**Why not `prometheus.io/port2`, `prometheus.io/port3`, …?**
No standard exists; it would require n independent annotations with no defined upper bound. Parsing is fragile and ordering is undefined if the annotation set is unsorted. The Kubernetes annotation key space doesn't guarantee iteration order.

**Why `prometheus.istio.io/` namespace?**
It's an Istio-owned namespace, already used for other Istio-specific hints, and makes the Istio-specific semantics unambiguous to operators.

**Backward compatibility:** The existing `prometheus.io/port` + `prometheus.io/path` single-endpoint flow is unchanged. If `prometheus.istio.io/scrape-targets` is absent, the webhook and agent behave exactly as today. Pods that were injected before this feature is available continue to work because old-format `ISTIO_PROMETHEUS_ANNOTATIONS` JSON remains valid.

---

## Required Changes

### 1. `pilot/cmd/pilot-agent/status/server.go`

#### `PrometheusScrapeConfiguration` (line ~507)

Add a `Targets` slice alongside the existing fields:

```go
type ScrapeTarget struct {
    Port string `json:"port"`
    Path string `json:"path"`
}

type PrometheusScrapeConfiguration struct {
    Scrape  string         `json:"scrape"`
    Path    string         `json:"path"`   // kept for backward compat
    Port    string         `json:"port"`   // kept for backward compat
    Targets []ScrapeTarget `json:"targets,omitempty"`
}
```

Old JSON (`{"scrape":"true","port":"8080","path":"/metrics"}`) unmarshals cleanly with `Targets == nil`. No breaking change.

#### `NewServer()` (line ~245)

After unmarshaling `ISTIO_PROMETHEUS_ANNOTATIONS`, normalize to the `Targets` representation:

```go
if len(prom.Targets) == 0 && prom.Port != "" {
    // legacy single-port: synthesize a one-element list
    prom.Targets = []ScrapeTarget{{Port: prom.Port, Path: prom.Path}}
}
// default path
for i, t := range prom.Targets {
    if t.Path == "" {
        prom.Targets[i].Path = "/metrics"
    }
    if t.Port == strconv.Itoa(int(config.StatusPort)) {
        return nil, fmt.Errorf("invalid prometheus scrape configuration: target port %s points at agent status port", t.Port)
    }
}
```

`Server.prometheus` type stays as `*PrometheusScrapeConfiguration`; no field type change needed.

#### `handleStats()` (line ~513)

Replace the single-endpoint block with a concurrent fan-out (see Merge Strategy below). The existing serial `io.Copy` for Envoy stats and agent metrics is unchanged.

---

### 2. `pkg/kube/inject/webhook.go`

#### `getPrometheusScrapeConfiguration()` (line ~959)

After reading the existing three `prometheus.io/*` annotations, also check `prometheus.istio.io/scrape-targets`:

```go
const prometheusIstioTargetsAnnotation = "prometheus_istio_io_scrape_targets"

// in switch:
case prometheusIstioTargetsAnnotation:
    cfg.RawTargets = val  // "8080:/metrics,9100:/metrics"
```

Parse `RawTargets` into `[]ScrapeTarget` here or in `applyPrometheusMerge`.

#### `applyPrometheusMerge()` (line ~884)

Build the `Targets` slice:
1. If `RawTargets` is set, parse it; each entry overrides/augments the single `Port`/`Path` fields.
2. If only the single-port annotation is present, produce the existing single-entry flow (no behavior change).
3. Validate every target port ≠ `targetPort` (15020).
4. JSON-encode the full struct (including `Targets`) into `ISTIO_PROMETHEUS_ANNOTATIONS`.

No change to the annotation rewrite block — `prometheus.io/port` is still set to `15020`.

---

## Merge Strategy

### Current behavior (N=1)

```text
agent metrics  →  io.Copy
Envoy stats    →  io.Copy
app endpoint   →  scrape() → io.Copy
```

All sequential. The response writer is the only synchronization point. `handleStats` never returns an HTTP error; any scrape failure is logged and skipped.

### Proposed behavior (N > 1)

When `len(s.prometheus.Targets) > 1`, `handleStats` dispatches to a fan-out helper that scrapes every target concurrently, buffers each response, and writes them into the response in `Targets` order. The single-target case (`len(Targets) <= 1`) keeps the existing streaming `io.Copy` hot path byte-for-byte unchanged, so legacy pods pay no concurrency or buffering overhead.

```go
func (s *Server) scrapeMultipleApps(r *http.Request) (expfmt.Format, [][]byte) {
    bodies := make([][]byte, len(s.prometheus.Targets))
    contentTypes := make([]string, len(s.prometheus.Targets))
    var wg sync.WaitGroup
    for i, t := range s.prometheus.Targets {
        wg.Add(1)
        go func(idx int, tgt ScrapeTarget) {
            defer wg.Done()
            url := fmt.Sprintf("http://localhost:%s%s", tgt.Port, tgt.Path)
            body, cancel, ct, err := s.scrape(url, r.Header)
            if cancel != nil { defer cancel() }
            if err != nil {
                metrics.AppScrapeErrors.Increment()
                return
            }
            defer body.Close()
            buf, readErr := io.ReadAll(body)
            if readErr != nil {
                metrics.AppScrapeErrors.Increment()
                return
            }
            bodies[idx] = buf
            contentTypes[idx] = ct
        }(i, t)
    }
    wg.Wait()
    // Pick the first successful target's format. With Targets[0] typically succeeding, this
    // reduces to Targets[0]'s format and preserves single-target semantics.
    format := FmtText
    for i := range bodies {
        if bodies[i] != nil {
            format = negotiateMetricsFormat(contentTypes[i])
            break
        }
    }
    return format, bodies
}
```

After agent-internal and Envoy metrics are written, the buffered app bodies are written in `Targets` order:

```go
openMetrics := strings.HasPrefix(string(format), expfmt.OpenMetricsType)
lastSuccess := -1
for i, b := range appBodies {
    if b != nil { lastSuccess = i }
}
for i, body := range appBodies {
    if body == nil { continue }
    out := body
    if !openMetrics || i != lastSuccess {
        out = stripOpenMetricsEOF(body)
    }
    w.Write(out)
}
```

**Ordering:** Results are written by `Targets` position (indexed slice, not completion order), so the response is deterministic regardless of which goroutine finishes first.

**Buffering vs. streaming:** Each body is read into memory because (a) the `# EOF` stripping rule below needs to inspect the trailer, and (b) writing in `Targets` order requires waiting for all goroutines anyway. `N` is expected to be small (≤10 in practice); memory cost is bounded by `N × response size`.

**Per-endpoint timeout:** The incoming `X-Prometheus-Scrape-Timeout-Seconds` value is forwarded per-goroutine via `scrape()`. Each target gets its own context and cancel function.

**Best-effort semantics:** A failed target (scrape error or read error) leaves `bodies[i] == nil`, increments `metrics.AppScrapeErrors` once, and does not abort the response. The merged output still returns 200 with partial data, consistent with the existing "we do not return any errors here" philosophy.

**Format negotiation:** The response `Content-Type` is the negotiated format of the first successful target. Because `Targets[0]` is treated as the primary endpoint (matching the legacy single-port contract), this equals `Targets[0]`'s format when `Targets[0]` succeeds, and falls back to the first successful target otherwise, or `FmtText` if every target fails.

**OpenMetrics `# EOF` handling:**
- When the negotiated response format is OpenMetrics, keep exactly one `# EOF` at the end: `stripOpenMetricsEOF` removes it from every body except the final successful one.
- When the response is text, strip `# EOF` from every body unconditionally; a text exposition must not carry OpenMetrics-specific terminators.

**Metric family name conflicts:** Two targets advertising the same metric family name will still produce a Prometheus parse error at the consumer, same as today's Envoy vs. app conflict behavior. Deduplication is out of scope; users are responsible for keeping target metric namespaces disjoint. The existing `TestStats` conflict test cases document this contract.

---

## Testing Plan

### Unit tests (`pilot/cmd/pilot-agent/status/server_test.go`)

Extend `TestStats` with new table cases:
- Two healthy endpoints, distinct metric families → merged output contains both families
- Two healthy endpoints, one produces openmetrics → EOF stripped from first, present in second
- First endpoint down, second healthy → second metrics present, `AppScrapeErrors` incremented
- Both endpoints down → only agent + Envoy metrics returned (best-effort)
- Multi-endpoint with one family conflict → Prometheus parse error (expected)

Extend `TestStatsError` to exercise the per-endpoint error path.

Add `TestParseScrapetargets` for the `prometheus.istio.io/scrape-targets` parsing logic (covers malformed inputs, empty path defaulting, whitespace trimming).

### Webhook unit tests (`pkg/kube/inject/webhook.go` → `inject_test.go`)

Add a new golden file (e.g., `testdata/hello-multi-port-metrics.yaml.injected`) verifying:
- `ISTIO_PROMETHEUS_ANNOTATIONS` encodes both targets
- `prometheus.io/port` is rewritten to `15020`
- `prometheus.io/path` is rewritten to `/stats/prometheus`

Extend `TestEnablePrometheusAggregation` (or add `TestMultiPortPrometheusAggregation`) for the two-target case.

### Integration tests (`tests/integration/telemetry/api/`)

Add a test case (following the `TestStatsFilter` pattern) that deploys a pod with two containers each exposing distinct metric families, then scrapes `:15020/stats/prometheus` and asserts both families appear. This exercises the full annotation → webhook → env var → handleStats → merged response path.

---

## PR Breakdown

**PR 1 — Data model and annotation parsing (no scrape behavior change)**
- `PrometheusScrapeConfiguration`: add `ScrapeTarget` and `Targets` field
- `NewServer()`: normalize legacy single-port into `Targets`; validate all target ports
- `getPrometheusScrapeConfiguration()` + `applyPrometheusMerge()`: parse and encode `prometheus.istio.io/scrape-targets`
- Unit tests for parsing and backward compat
- Golden file update for multi-port injection

This PR is purely additive; with `Targets` populated but `handleStats` still only reading `s.prometheus.Port`/`Path`, existing behavior is identical.

**PR 2 — Concurrent fan-out in `handleStats`**
- Replace single-endpoint block with goroutine fan-out
- `newEOFStrippingReader` for OpenMetrics interop
- Update `handleStats` to read from `s.prometheus.Targets`
- Unit tests: multi-endpoint happy path, partial failure, format negotiation
- This PR requires PR 1 to be merged first (depends on `Targets` field)

**PR 3 — Integration test**
- New multi-container echo deployment in `tests/integration/telemetry/api/`
- Assert merged scrape contains metrics from both containers
- Can be developed against PR 2's branch but reviewed/merged after PR 2 lands
