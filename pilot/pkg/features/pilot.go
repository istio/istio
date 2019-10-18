// Copyright 2019 Istio Authors
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

package features

import (
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"

	"istio.io/pkg/env"
)

var (
	// CertDir is the default location for mTLS certificates used by pilot.
	// Defaults to /etc/certs, matching k8s template. Can be used if you run pilot
	// as a regular user on a VM or test environment.
	CertDir = env.RegisterStringVar("PILOT_CERT_DIR", "", "").Get()

	MaxConcurrentStreams = env.RegisterIntVar(
		"ISTIO_GPRC_MAXSTREAMS",
		100000,
		"Sets the maximum number of concurrent grpc streams.",
	).Get()

	TraceSampling = env.RegisterFloatVar(
		"PILOT_TRACE_SAMPLING",
		100.0,
		"Sets the mesh-wide trace sampling percentage. Should be 0.0 - 100.0. Precision to 0.01. "+
			"Default is 100, not recommended for production use.",
	).Get()

	PushThrottle = env.RegisterIntVar(
		"PILOT_PUSH_THROTTLE",
		100,
		"Limits the number of concurrent pushes allowed. On larger machines this can be increased for faster pushes",
	).Get()

	// DebugConfigs controls saving snapshots of configs for /debug/adsz.
	// Defaults to false, can be enabled with PILOT_DEBUG_ADSZ_CONFIG=1
	// For larger clusters it can increase memory use and GC - useful for small tests.
	DebugConfigs = env.RegisterBoolVar("PILOT_DEBUG_ADSZ_CONFIG", false, "").Get()

	DebounceAfter = env.RegisterDurationVar(
		"PILOT_DEBOUNCE_AFTER",
		100*time.Millisecond,
		"The delay added to config/registry events for debouncing. This will delay the push by "+
			"at least this internal. If no change is detected within this period, the push will happen, "+
			" otherwise we'll keep delaying until things settle, up to a max of PILOT_DEBOUNCE_MAX.",
	).Get()

	DebounceMax = env.RegisterDurationVar(
		"PILOT_DEBOUNCE_MAX",
		10*time.Second,
		"The maximum amount of time to wait for events while debouncing. If events keep showing up with no breaks "+
			"for this time, we'll trigger a push.",
	).Get()

	EnableEDSDebounce = env.RegisterBoolVar(
		"PILOT_ENABLE_EDS_DEBOUNCE",
		true,
		"If enabled, Pilot will include EDS pushes in the push debouncing, configured by PILOT_DEBOUNCE_AFTER and PILOT_DEBOUNCE_MAX."+
			" EDS pushes may be delayed, but there will be fewer pushes. By default this is enabled",
	)

	// BaseDir is the base directory for locating configs.
	// File based certificates are located under $BaseDir/etc/certs/. If not set, the original 1.0 locations will
	// be used, "/"
	BaseDir = "BASE"

	// HTTP10 will add "accept_http_10" to http outbound listeners. Can also be set only for specific sidecars via meta.
	//
	// Alpha in 1.1, may become the default or be turned into a Sidecar API or mesh setting. Only applies to namespaces
	// where Sidecar is enabled.
	HTTP10 = env.RegisterBoolVar(
		"PILOT_HTTP10",
		false,
		"Enables the use of HTTP 1.0 in the outbound HTTP listeners, to support legacy applications.",
	).Get()

	initialFetchTimeoutVar = env.RegisterDurationVar(
		"PILOT_INITIAL_FETCH_TIMEOUT",
		0,
		"Specifies the initial_fetch_timeout for config. If this time is reached without "+
			"a response to the config requested by Envoy, the Envoy will move on with the init phase. "+
			"This prevents envoy from getting stuck waiting on config during startup.",
	)
	InitialFetchTimeout = func() *duration.Duration {
		timeout, f := initialFetchTimeoutVar.Lookup()
		if !f {
			return nil
		}
		return ptypes.DurationProto(timeout)
	}()

	terminationDrainDurationVar = env.RegisterIntVar(
		"TERMINATION_DRAIN_DURATION_SECONDS",
		5,
		"The amount of time allowed for connections to complete on pilot-agent shutdown. "+
			"On receiving SIGTERM or SIGINT, pilot-agent tells the active Envoy to start draining, "+
			"preventing any new connections and allowing existing connections to complete. It then "+
			"sleeps for the TerminationDrainDuration and then kills any remaining active Envoy processes.",
	)
	TerminationDrainDuration = func() time.Duration {
		return time.Second * time.Duration(terminationDrainDurationVar.Get())
	}

	EnableFallthroughRoute = env.RegisterBoolVar(
		"PILOT_ENABLE_FALLTHROUGH_ROUTE",
		true,
		"EnableFallthroughRoute provides an option to add a final wildcard match for routes. "+
			"When ALLOW_ANY traffic policy is used, a Passthrough cluster is used. "+
			"When REGISTRY_ONLY traffic policy is used, a 502 error is returned.",
	)

	// DisableXDSMarshalingToAny provides an option to disable the "xDS marshaling to Any" feature ("on" by default).
	DisableXDSMarshalingToAny = env.RegisterBoolVar(
		"PILOT_DISABLE_XDS_MARSHALING_TO_ANY",
		false,
		"",
	).Get()

	// EnableMysqlFilter enables injection of `envoy.filters.network.mysql_proxy` in the filter chain.
	// Pilot injects this outbound filter if the service port name is `mysql`.
	EnableMysqlFilter = env.RegisterBoolVar(
		"PILOT_ENABLE_MYSQL_FILTER",
		false,
		"EnableMysqlFilter enables injection of `envoy.filters.network.mysql_proxy` in the filter chain.",
	)

	// EnableRedisFilter enables injection of `envoy.filters.network.redis_proxy` in the filter chain.
	// Pilot injects this outbound filter if the service port name is `redis`.
	EnableRedisFilter = env.RegisterBoolVar(
		"PILOT_ENABLE_REDIS_FILTER",
		false,
		"EnableRedisFilter enables injection of `envoy.filters.network.redis_proxy` in the filter chain.",
	)

	// UseRemoteAddress sets useRemoteAddress to true for side car outbound listeners so that it picks up the localhost
	// address of the sender, which is an internal address, so that trusted headers are not sanitized.
	UseRemoteAddress = env.RegisterBoolVar(
		"PILOT_SIDECAR_USE_REMOTE_ADDRESS",
		false,
		"UseRemoteAddress sets useRemoteAddress to true for side car outbound listeners.",
	)

	// UseIstioJWTFilter enables to use Istio JWT filter as a fall back. Pilot injects the Istio JWT
	// filter to the filter chains if this is set to true.
	// TODO(yangminzhu): Remove after fully migrate to Envoy JWT filter.
	UseIstioJWTFilter = env.RegisterBoolVar(
		"USE_ISTIO_JWT_FILTER",
		false,
		"Use the Istio JWT filter for JWT token verification.")

	// SkipValidateTrustDomain tells the server proxy to not to check the peer's trust domain when
	// mTLS is enabled in authentication policy.
	SkipValidateTrustDomain = env.RegisterBoolVar(
		"PILOT_SKIP_VALIDATE_TRUST_DOMAIN",
		false,
		"Skip validating the peer is from the same trust domain when mTLS is enabled in authentication policy")

	RestrictPodIPTrafficLoops = env.RegisterBoolVar(
		"PILOT_RESTRICT_POD_UP_TRAFFIC_LOOP",
		true,
		"If enabled, this will block inbound traffic from matching outbound listeners, which "+
			"could result in an infinite loop of traffic. This option is only provided for backward compatibility purposes "+
			"and will be removed in the near future.",
	)

	EnableProtocolSniffingForOutbound = env.RegisterBoolVar(
		"PILOT_ENABLE_PROTOCOL_SNIFFING_FOR_OUTBOUND",
		true,
		"If enabled, protocol sniffing will be used for outbound listeners whose port protocol is not specified or unsupported",
	)

	EnableProtocolSniffingForInbound = env.RegisterBoolVar(
		"PILOT_ENABLE_PROTOCOL_SNIFFING_FOR_INBOUND",
		false,
		"If enabled, protocol sniffing will be used for inbound listeners whose port protocol is not specified or unsupported",
	)

	ScopePushes = env.RegisterBoolVar(
		"PILOT_SCOPE_PUSHES",
		true,
		"If enabled, pilot will attempt to limit unnecessary pushes by determining what proxies "+
			"a config or endpoint update will impact.",
	)

	ScopeGatewayToNamespace = env.RegisterBoolVar(
		"PILOT_SCOPE_GATEWAY_TO_NAMESPACE",
		false,
		"If enabled, a gateway workload can only select gateway resources in the same namespace. "+
			"Gateways with same selectors in different namespaces will not be applicable.",
	)

	RespectDNSTTL = env.RegisterBoolVar(
		"PILOT_RESPECT_DNS_TTL",
		true,
		"If enabled, DNS based clusters will respect the TTL of the DNS, rather than polling at a fixed rate. "+
			"This option is only provided for backward compatibility purposes and will be removed in the near future.",
	)

	InboundProtocolDetectionTimeout = env.RegisterDurationVar(
		"PILOT_INBOUND_PROTOCOL_DETECTION_TIMEOUT",
		1*time.Second,
		"Protocol detection timeout for inbound listener",
	).Get()

	EnableHeadlessService = env.RegisterBoolVar(
		"PILOT_ENABLE_HEADLESS_SERVICE_POD_LISTENERS",
		true,
		"If enabled, for a headless service/stateful set in Kubernetes, pilot will generate an "+
			"outbound listener for each pod in a headless service. This feature should be disabled "+
			"if headless services have a large number of pods.",
	)

	BlockHTTPonHTTPSPort = env.RegisterBoolVar(
		"PILOT_BLOCK_HTTP_ON_443",
		true,
		"If enabled, any HTTP services will be blocked on HTTPS port (443). If this is disabled, any "+
			"HTTP service on port 443 could block all external traffic",
	).Get()

	EnableDistributionTracking = env.RegisterBoolVar(
		"PILOT_ENABLE_CONFIG_DISTRIBUTION_TRACKING",
		true,
		"If enabled, Pilot will assign meaningful nonces to each Envoy configuration message, and allow "+
			"users to interrogate which envoy has which config from the debug interface.",
	).Get()

	DistributionHistoryRetention = env.RegisterDurationVar(
		"PILOT_DISTRIBUTION_HISTORY_RETENTION",
		time.Minute*1,
		"If enabled, Pilot will keep track of old versions of distributed config for this duration.",
	).Get()

	EnableUnsafeRegex = env.RegisterBoolVar(
		"PILOT_ENABLE_UNSAFE_REGEX",
		false,
		"If enabled, pilot will generate Envoy configuration that does not use safe_regex "+
			"but the older, deprecated regex field. This should only be enabled to support "+
			"legacy deployments that have not yet been migrated to the new safe regular expressions.",
	)
)

var (
	// TODO: define all other default ports here, add docs

	// DefaultPortHTTPProxy is used as for HTTP PROXY mode. Can be overridden by ProxyHttpPort in mesh config.
	DefaultPortHTTPProxy = 15002
)
