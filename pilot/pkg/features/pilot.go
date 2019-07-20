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
	"strconv"
	"time"

	"github.com/gogo/protobuf/types"

	"istio.io/pkg/log"

	"istio.io/pkg/env"
)

var (
	// CertDir is the default location for mTLS certificates used by pilot.
	// Defaults to /etc/certs, matching k8s template. Can be used if you run pilot
	// as a regular user on a VM or test environment.
	CertDir = env.RegisterStringVar("PILOT_CERT_DIR", "", "").Get()

	// MaxConcurrentStreams indicates pilot max grpc concurrent streams.
	// Default is 100000.
	MaxConcurrentStreams = env.RegisterIntVar("ISTIO_GPRC_MAXSTREAMS", 100000, "").Get()

	// TraceSampling sets mesh-wide trace sampling
	// percentage, should be 0.0 - 100.0 Precision to 0.01
	// Default is 100%, not recommended for production use.
	TraceSampling = env.RegisterFloatVar("PILOT_TRACE_SAMPLING", 100.0, "").Get()

	// PushThrottle limits the qps of the actual push. Default is 10 pushes per second.
	// On larger machines you can increase this to get faster push.
	PushThrottle = env.RegisterIntVar("PILOT_PUSH_THROTTLE", 10, "").Get()

	// PushBurst limits the burst of the actual push. Default is 100.
	PushBurst = env.RegisterIntVar("PILOT_PUSH_BURST", 100, "").Get()

	// DebugConfigs controls saving snapshots of configs for /debug/adsz.
	// Defaults to false, can be enabled with PILOT_DEBUG_ADSZ_CONFIG=1
	// For larger clusters it can increase memory use and GC - useful for small tests.
	DebugConfigs = env.RegisterBoolVar("PILOT_DEBUG_ADSZ_CONFIG", false, "").Get()

	// RefreshDuration is the duration of periodic refresh, in case events or cache invalidation fail.
	// Example: "300ms", "10s" or "2h45m".
	// Default is 0 (disabled).
	RefreshDuration = env.RegisterDurationVar("V2_REFRESH", 0, "").Get()

	// DebounceAfter is the delay added to events to wait
	// after a registry/config event for debouncing.
	// This will delay the push by at least this interval, plus
	// the time getting subsequent events. If no change is
	// detected the push will happen, otherwise we'll keep
	// delaying until things settle.
	// Default is 100ms, Example: "300ms", "10s" or "2h45m".
	DebounceAfter = env.RegisterDurationVar("PILOT_DEBOUNCE_AFTER", 100*time.Millisecond, "").Get()

	// DebounceMax is the maximum time to wait for events
	// while debouncing. Defaults to 10 seconds. If events keep
	// showing up with no break for this time, we'll trigger a push.
	// Default is 10s, Example: "300ms", "10s" or "2h45m".
	DebounceMax = env.RegisterDurationVar("PILOT_DEBOUNCE_MAX", 10*time.Second, "").Get()

	// BaseDir is the base directory for locating configs.
	// File based certificates are located under $BaseDir/etc/certs/. If not set, the original 1.0 locations will
	// be used, "/"
	BaseDir = "BASE"

	// HTTP10 enables the use of HTTP10 in the outbound HTTP listeners, to support legacy applications.
	// Will add "accept_http_10" to http outbound listeners. Can also be set only for specific sidecars via meta.
	//
	// Alpha in 1.1, may become the default or be turned into a Sidecar API or mesh setting. Only applies to namespaces
	// where Sidecar is enabled.
	HTTP10 = env.RegisterBoolVar("PILOT_HTTP10", false, "").Get()

	initialFetchTimeoutVar = env.RegisterDurationVar(
		"PILOT_INITIAL_FETCH_TIMEOUT",
		0,
		"Specifies the initial_fetch_timeout for config. If this time is reached without "+
			"a response to the config requested by Envoy, the Envoy will move on with the init phase. "+
			"This prevents envoy from getting stuck waiting on config during startup.",
	)
	InitialFetchTimeout = types.DurationProto(initialFetchTimeoutVar.Get())

	// TerminationDrainDuration is the amount of time allowed for connections to complete on pilot-agent shutdown.
	// On receiving SIGTERM or SIGINT, pilot-agent tells the active Envoy to start draining,
	// preventing any new connections and allowing existing connections to complete. It then
	// sleeps for the TerminationDrainDuration and then kills any remaining active Envoy processes.
	terminationDrainDurationVar = env.RegisterStringVar("TERMINATION_DRAIN_DURATION_SECONDS", "", "")
	TerminationDrainDuration    = func() time.Duration {
		defaultDuration := time.Second * 5
		if terminationDrainDurationVar.Get() == "" {
			return defaultDuration
		}
		duration, err := strconv.Atoi(terminationDrainDurationVar.Get())
		if err != nil {
			log.Warnf("unable to parse env var %v, using default of %v.", terminationDrainDurationVar.Get(), defaultDuration)
			return defaultDuration
		}
		return time.Second * time.Duration(duration)
	}

	// EnableLocalityLoadBalancing provides an option to enable the LocalityLoadBalancerSetting feature
	// as well as prioritizing the sending of traffic to a local locality. Set the environment variable to any value to enable.
	// This is an experimental feature.
	enableLocalityLoadBalancingVar = env.RegisterStringVar("PILOT_ENABLE_LOCALITY_LOAD_BALANCING", "", "")
	EnableLocalityLoadBalancing    = func() bool {
		return len(enableLocalityLoadBalancingVar.Get()) != 0
	}

	enableFallthroughRouteVar = env.RegisterBoolVar(
		"PILOT_ENABLE_FALLTHROUGH_ROUTE",
		true,
		"EnableFallthroughRoute provides an option to add a final wildcard match for routes. "+
			"When ALLOW_ANY traffic policy is used, a Passthrough cluster is used. "+
			"When REGISTRY_ONLY traffic policy is used, a 502 error is returned.",
	)
	EnableFallthroughRoute = enableFallthroughRouteVar.Get

	// DisableXDSMarshalingToAny provides an option to disable the "xDS marshaling to Any" feature ("on" by default).
	disableXDSMarshalingToAnyVar = env.RegisterStringVar("PILOT_DISABLE_XDS_MARSHALING_TO_ANY", "", "")
	DisableXDSMarshalingToAny    = func() bool {
		return disableXDSMarshalingToAnyVar.Get() == "1"
	}

	// EnableMysqlFilter enables injection of `envoy.filters.network.mysql_proxy` in the filter chain.
	// Pilot injects this outbound filter if the service port name is `mysql`.
	EnableMysqlFilter = enableMysqlFilter.Get
	enableMysqlFilter = env.RegisterBoolVar(
		"PILOT_ENABLE_MYSQL_FILTER",
		false,
		"EnableMysqlFilter enables injection of `envoy.filters.network.mysql_proxy` in the filter chain.")

	// EnableRedisFilter enables injection of `envoy.filters.network.redis_proxy` in the filter chain.
	// Pilot injects this outbound filter if the service port name is `redis`.
	EnableRedisFilter = enableRedisFilter.Get
	enableRedisFilter = env.RegisterBoolVar(
		"PILOT_ENABLE_REDIS_FILTER",
		false,
		"EnableRedisFilter enables injection of `envoy.filters.network.redis_proxy` in the filter chain.")

	// UseRemoteAddress sets useRemoteAddress to true for side car outbound listeners so that it picks up the localhost
	// address of the sender, which is an internal address, so that trusted headers are not sanitized.
	UseRemoteAddress = useRemoteAddress.Get
	useRemoteAddress = env.RegisterBoolVar(
		"PILOT_SIDECAR_USE_REMOTE_ADDRESS",
		false,
		"UseRemoteAddress sets useRemoteAddress to true for side car outbound listeners.")

	// UseIstioJWTFilter enables to use Istio JWT filter as a fall back. Pilot injects the Istio JWT
	// filter to the filter chains if this is set to true.
	// TODO(yangminzhu): Remove after fully migrate to Envoy JWT filter.
	UseIstioJWTFilter = env.RegisterBoolVar(
		"USE_ISTIO_JWT_FILTER",
		false,
		"Use the Istio JWT filter for JWT token verification.")
)

var (
	// TODO: define all other default ports here, add docs

	// DefaultPortHTTPProxy is used as for HTTP PROXY mode. Can be overridden by ProxyHttpPort in mesh config.
	DefaultPortHTTPProxy = 15002
)
