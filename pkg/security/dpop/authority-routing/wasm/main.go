//go:build tinygo

// WASM plugin: rewrites short :authority names to FQDN and sets the
// x-envoy-upstream-rq-cluster header so Envoy can route dynamically.
//
// Build:
//   tinygo build -o authority-rewrite.wasm -scheduler=none -target=wasi ./main.go
//
// Push to OCI registry:
//   crane push authority-rewrite.wasm your-registry/authority-rewrite:v1.0.0
//
// Deploy via wasm-plugin.yaml.
package main

import (
	"strings"

	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm"
	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm/types"
)

func main() {}

func init() {
	proxywasm.SetVMContext(&vmContext{})
}

type vmContext struct{}

func (*vmContext) OnVMStart(_ int) types.OnVMStartStatus { return types.OnVMStartStatusOK }
func (*vmContext) NewPluginContext(contextID uint32) types.PluginContext {
	return &pluginContext{
		namespace:     "default",
		clusterDomain: "svc.cluster.local",
		defaultPort:   "80",
	}
}

type pluginContext struct {
	types.DefaultPluginContext
	namespace     string
	clusterDomain string
	defaultPort   string
}

func (p *pluginContext) OnPluginStart(_ int) types.OnPluginStartStatus {
	data, err := proxywasm.GetPluginConfiguration()
	if err != nil || len(data) == 0 {
		return types.OnPluginStartStatusOK
	}
	// Simple "key: value" YAML-ish parsing — avoids JSON dependency in WASM.
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "namespace":
			p.namespace = strings.TrimSpace(v)
		case "clusterDomain":
			p.clusterDomain = strings.TrimSpace(v)
		case "defaultPort":
			p.defaultPort = strings.TrimSpace(v)
		}
	}
	return types.OnPluginStartStatusOK
}

func (p *pluginContext) NewHttpContext(contextID uint32) types.HttpContext {
	return &httpContext{plugin: p}
}

type httpContext struct {
	types.DefaultHttpContext
	plugin *pluginContext
}

func (h *httpContext) OnHttpRequestHeaders(_ int, _ bool) types.Action {
	authority, err := proxywasm.GetHttpRequestHeader(":authority")
	if err != nil || authority == "" {
		return types.ActionContinue
	}

	svcName, port := splitHostPort(authority, h.plugin.defaultPort)

	// Only rewrite short names — names with dots are already FQDNs.
	if strings.Contains(svcName, ".") {
		return types.ActionContinue
	}

	// Validate it looks like a Kubernetes service name (letters, digits, hyphens).
	if !isShortServiceName(svcName) {
		return types.ActionContinue
	}

	// Allow per-request namespace override.
	ns := h.plugin.namespace
	if hint, err := proxywasm.GetHttpRequestHeader("x-target-namespace"); err == nil && hint != "" {
		ns = hint
		_ = proxywasm.RemoveHttpRequestHeader("x-target-namespace")
	}

	fqdn := svcName + "." + ns + "." + h.plugin.clusterDomain

	// Cluster name format from pilot/pkg/model/service.go BuildSubsetKey:
	//   direction|port|subset|hostname  →  outbound|80||my-svc.ns.svc.cluster.local
	cluster := "outbound|" + port + "||" + fqdn

	// Rewrite :authority to FQDN.
	if err := proxywasm.ReplaceHttpRequestHeader(":authority", fqdn); err != nil {
		proxywasm.LogErrorf("[authority-rewrite] replace :authority failed: %v", err)
		return types.ActionContinue
	}

	// Set the cluster routing header — consumed by Envoy, stripped before upstream.
	if err := proxywasm.ReplaceHttpRequestHeader("x-envoy-upstream-rq-cluster", cluster); err != nil {
		proxywasm.LogErrorf("[authority-rewrite] set cluster header failed: %v", err)
	}

	proxywasm.LogInfof("[authority-rewrite] %s -> fqdn=%s cluster=%s", authority, fqdn, cluster)
	return types.ActionContinue
}

// splitHostPort splits "host:port" → (host, port), using defaultPort when absent.
func splitHostPort(authority, defaultPort string) (string, string) {
	// LastIndex handles IPv6 addresses like [::1]:8080 correctly.
	idx := strings.LastIndex(authority, ":")
	if idx == -1 {
		return authority, defaultPort
	}
	host, port := authority[:idx], authority[idx+1:]
	if port == "" {
		return host, defaultPort
	}
	return host, port
}

// isShortServiceName returns true for valid Kubernetes service names
// (lowercase alphanumeric + hyphens, no leading/trailing hyphen).
func isShortServiceName(name string) bool {
	if len(name) == 0 || len(name) > 63 {
		return false
	}
	for i, c := range name {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' && i > 0 && i < len(name)-1:
		default:
			return false
		}
	}
	return true
}
