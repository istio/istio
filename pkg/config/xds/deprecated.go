package xds

import "github.com/envoyproxy/go-control-plane/pkg/wellknown"

var (
	// DeprecatedFilterNames is to support both canonical filter names
	// and deprecated filter names for backward compatibility. Istiod
	// generates canonical filter names.
	DeprecatedFilterNames = map[string]string{
		wellknown.Buffer:                      "envoy.buffer",
		wellknown.CORS:                        "envoy.cors",
		"envoy.filters.http.csrf":             "envoy.csrf",
		wellknown.Dynamo:                      "envoy.http_dynamo_filter",
		wellknown.HTTPExternalAuthorization:   "envoy.ext_authz",
		wellknown.Fault:                       "envoy.fault",
		wellknown.GRPCHTTP1Bridge:             "envoy.grpc_http1_bridge",
		wellknown.GRPCJSONTranscoder:          "envoy.grpc_json_transcoder",
		wellknown.GRPCWeb:                     "envoy.grpc_web",
		wellknown.Gzip:                        "envoy.gzip",
		wellknown.HealthCheck:                 "envoy.health_check",
		wellknown.IPTagging:                   "envoy.ip_tagging",
		wellknown.Lua:                         "envoy.lua",
		wellknown.HTTPRateLimit:               "envoy.rate_limit",
		wellknown.Router:                      "envoy.router",
		wellknown.Squash:                      "envoy.squash",
		wellknown.HttpInspector:               "envoy.listener.http_inspector",
		wellknown.OriginalDestination:         "envoy.listener.original_dst",
		"envoy.filters.listener.original_src": "envoy.listener.original_src",
		wellknown.ProxyProtocol:               "envoy.listener.proxy_protocol",
		wellknown.TlsInspector:                "envoy.listener.tls_inspector",
		wellknown.ClientSSLAuth:               "envoy.client_ssl_auth",
		wellknown.ExternalAuthorization:       "envoy.ext_authz",
		wellknown.HTTPConnectionManager:       "envoy.http_connection_manager",
		wellknown.MongoProxy:                  "envoy.mongo_proxy",
		wellknown.RateLimit:                   "envoy.ratelimit",
		wellknown.RedisProxy:                  "envoy.redis_proxy",
		wellknown.TCPProxy:                    "envoy.tcp_proxy",
	}

	ReverseDeprecatedFilterNames = reverse(DeprecatedFilterNames)
)

func reverse(names map[string]string) map[string]string {
	resp := make(map[string]string, len(names))
	for k, v := range names {
		resp[v] = k
	}
	return resp
}
