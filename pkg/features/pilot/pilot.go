//  Copyright 2018 Istio Authors
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

package pilot

import (
	"os"
	"strconv"
	"time"

	"istio.io/istio/pkg/log"
)

var (
	// CertDir is the default location for mTLS certificates used by pilot.
	// Defaults to /etc/certs, matching k8s template. Can be used if you run pilot
	// as a regular user on a VM or test environment.
	CertDir = os.Getenv("PILOT_CERT_DIR")

	// MaxConcurrentStreams indicates pilot max grpc concurrent streams.
	// Default is 100k.
	MaxConcurrentStreams = os.Getenv("ISTIO_GPRC_MAXSTREAMS")

	// TraceSampling sets mesh-wide trace sampling
	// percentage, should be 0.0 - 100.0 Precision to 0.01
	// Default is 100%, not recommended for production use.
	TraceSampling = os.Getenv("PILOT_TRACE_SAMPLING")

	// PushThrottle limits the qps of the actual push. Default is 10 pushes per second.
	// On larger machines you can increase this to get faster push.
	PushThrottle = os.Getenv("PILOT_PUSH_THROTTLE")

	// PushBurst limits the burst of the actual push. Default is 100.
	PushBurst = os.Getenv("PILOT_PUSH_BURST")

	// DebugConfigs controls saving snapshots of configs for /debug/adsz.
	// Defaults to false, can be enabled with PILOT_DEBUG_ADSZ_CONFIG=1
	// For larger clusters it can increase memory use and GC - useful for small tests.
	DebugConfigs = os.Getenv("PILOT_DEBUG_ADSZ_CONFIG") == "1"

	// RefreshDuration is the duration of periodic refresh, in case events or cache invalidation fail.
	// Example: "300ms", "10s" or "2h45m".
	// Default is 0 (disabled).
	RefreshDuration = os.Getenv("V2_REFRESH")

	// DebounceAfter is the delay added to events to wait
	// after a registry/config event for debouncing.
	// This will delay the push by at least this interval, plus
	// the time getting subsequent events. If no change is
	// detected the push will happen, otherwise we'll keep
	// delaying until things settle.
	// Default is 100ms, Example: "300ms", "10s" or "2h45m".
	DebounceAfter = os.Getenv("PILOT_DEBOUNCE_AFTER")

	// DebounceMax is the maximum time to wait for events
	// while debouncing. Defaults to 10 seconds. If events keep
	// showing up with no break for this time, we'll trigger a push.
	// Default is 10s, Example: "300ms", "10s" or "2h45m".
	DebounceMax = os.Getenv("PILOT_DEBOUNCE_MAX")

	// DisableEDSIsolation provides an option to disable the feature
	// of EDS isolation which is enabled by default from Istio 1.1 and
	// go back to the legacy behavior of previous releases.
	// If not set, Pilot will return the endpoints for a proxy in an isolated namespace.
	// Set the environment variable to any value to disable.
	DisableEDSIsolation = os.Getenv("PILOT_DISABLE_EDS_ISOLATION")

	// AzDebug indicates whether to log service registry az info.
	AzDebug = os.Getenv("VERBOSE_AZ_DEBUG") == "1"

	// NetworkScopes isolates namespaces, limiting configuration for
	// egress and other mesh services to only hosts defined in same namespace or
	// 'admin' namespaces. Using services from any other namespaces will require the new NetworkScope
	// config. In most cases 'istio-system' should be included. Comma separated (ns1,ns2,istio-system)
	NetworkScopes = os.Getenv("DEFAULT_NAMESPACE_DEPENDENCIES")

	// BaseDir is the base directory for locating configs.
	// File based certificates are located under $BaseDir/etc/certs/. If not set, the original 1.0 locations will
	// be used, "/"
	BaseDir = "BASE"

	// HTTP10 enables the use of HTTP10 in the outbound HTTP listeners, to support legacy applications.
	// Will add "accept_http_10" to http outbound listeners. Can also be set only for specific sidecars via meta.
	//
	// Alpha in 1.1, may become the default or be turned into a Sidecar API or mesh setting. Only applies to namespaces
	// where Sidecar is enabled.
	HTTP10 = os.Getenv("PILOT_HTTP10") == "1"

	// TerminationDrainDuration is the amount of time allowed for connections to complete on pilot-agent shutdown.
	// On receiving SIGTERM or SIGINT, pilot-agent tells the active Envoy to start draining,
	// preventing any new connections and allowing existing connections to complete. It then
	// sleeps for the TerminationDrainDuration and then kills any remaining active Envoy processes.
	TerminationDrainDuration = func() time.Duration {
		defaultDuration := time.Second * 5
		if os.Getenv("TERMINATION_DRAIN_DURATION_SECONDS") == "" {
			return defaultDuration
		}
		duration, err := strconv.Atoi(os.Getenv("TERMINATION_DRAIN_DURATION_SECONDS"))
		if err != nil {
			log.Warnf("unable to parse env var %v, using default of %v.", os.Getenv("TERMINATION_DRAIN_DURATION_SECONDS"), defaultDuration)
			return defaultDuration
		}
		return time.Second * time.Duration(duration)
	}

	// EnableCDSPrecomputation provides an option to enable precomputation
	// of CDS output for all namespaces at the start of a push cycle.
	// While it reduces CPU, it comes at the cost of increased memory usage
	EnableCDSPrecomputation = func() bool {
		return len(os.Getenv("PILOT_ENABLE_CDS_PRECOMPUTATION")) != 0
	}

	// EnableLocalityLoadBalancing provides an option to enable the LocalityLoadBalancerSetting feature
	// as well as prioritizing the sending of traffic to a local locality. Set the environment variable to any value to enable.
	// This is an experimental feature.
	EnableLocalityLoadBalancing = func() bool {
		return len(os.Getenv("PILOT_ENABLE_LOCALITY_LOAD_BALANCING")) != 0
	}

	// EnableWaitCacheSync provides an option to specify whether it should wait
	// for cache sync before Pilot bootstrap. Set env PILOT_ENABLE_WAIT_CACHE_SYNC = 0 to disable it.
	EnableWaitCacheSync = os.Getenv("PILOT_ENABLE_WAIT_CACHE_SYNC") != "0"

	// EnableFallthroughRoute provides an option to add a final wildcard match for routes.
	// When ALLOW_ANY traffic policy is used, a Passthrough cluster is used.
	// When REGISTRY_ONLY traffic policy is used, a 502 error is returned.
	EnableFallthroughRoute = func() bool {
		val, set := os.LookupEnv("PILOT_ENABLE_FALLTHROUGH_ROUTE")
		return val == "1" || !set
	}

	// DisablePartialRouteResponse provides an option to disable a partial route response. This
	// will cause Pilot to send an error if any routes are invalid. The default behavior (without
	// this flag) is to just skip the invalid route.
	DisablePartialRouteResponse = os.Getenv("PILOT_DISABLE_PARTIAL_ROUTE_RESPONSE") == "1"

	// DisableEmptyRouteResponse provides an option to disable a partial route response. This
	// will cause Pilot to ignore a route request if Pilot generates a nil route (due to an error).
	// This may cause Envoy to wait forever for the route, blocking listeners from receiving traffic.
	// The default behavior (without this flag set) is to explicitly send an empty route. This
	// will break routing for that particular route, but allow others on the same listener to work.
	DisableEmptyRouteResponse = os.Getenv("PILOT_DISABLE_EMPTY_ROUTE_RESPONSE") == "1"

	// DisableXDSMarshalingToAny provides an option to disable the "xDS marshaling to Any" feature ("on" by default).
	DisableXDSMarshalingToAny = func() bool {
		return os.Getenv("PILOT_DISABLE_XDS_MARSHALING_TO_ANY") == "1"
	}

	// InitialConnectionWindowSize specifies the window size to use for http2 connections
	// Must be 65535 - 2147483647, default 268435456 (256mb)
	initialConnectionWindowSize = func() int {
		raw, f := os.LookupEnv("PILOT_INITIAL_CONNECTION_WINDOW_SIZE")
		if !f {
			return 0
		}
		i, err := strconv.Atoi(raw)
		if err != nil {
			log.Warnf("failed to parse PILOT_INITIAL_CONNECTION_WINDOW_SIZE: %v", err)
			return 0
		}
		if i < 65535 || i > 2147483647 {
			log.Warnf("PILOT_INITIAL_CONNECTION_WINDOW_SIZE invalid, must be 65535 - 2147483647: %v", i)
			return 0
		}
		return i
	}
	InitialConnectionWindowSize = initialConnectionWindowSize()

	// InitialStreamWindowSize specifies the window size to use for http2 connections
	// Must be 65535 - 2147483647, default 268435456
	initialStreamWindowSize = func() int {
		raw, f := os.LookupEnv("PILOT_INITIAL_STREAM_WINDOW_SIZE")
		if !f {
			return 0
		}
		i, err := strconv.Atoi(raw)
		if err != nil {
			log.Warnf("failed to parse PILOT_INITIAL_STREAM_WINDOW_SIZE: %v", err)
			return 0
		}
		if i < 65535 || i > 2147483647 {
			log.Warnf("PILOT_INITIAL_STREAM_WINDOW_SIZE invalid, must be 65535 - 2147483647: %v", i)
			return 0
		}
		return i
	}
	InitialStreamWindowSize = initialStreamWindowSize()

	// DisableSplitHorizonEdsProxyNetworkCompare provides an option to disable
	// matching proxy and pod network id.
	DisableSplitHorizonEdsProxyNetworkCompare = func() bool {
		return os.Getenv("PILOT_DISABLE_SPLIT_HORIZON_EDS_NETWORK_COMPARE") == "1"
	}

	// EnableMysqlFilter enables injection of `envoy.filters.network.mysql_proxy` in the filter chain.
	// Pilot injects this outbound filter if the service port name is `mysql`.
	EnableMysqlFilter = os.Getenv("PILOT_ENABLE_MYSQL_FILTER") == "1"

	// RestrictPodIPTrafficLoops if enabled, this will block inbound traffic from matching outbound listeners, which
	// could result in an infinite loop of traffic. This option is only provided for backward compatibility purposes
	// and will be removed in the near future.
	RestrictPodIPTrafficLoops = func() bool {
		val, f := os.LookupEnv("PILOT_RESTRICT_POD_UP_TRAFFIC_LOOP")
		if !f {
			// Default to enabled
			return true
		}
		enabled, err := strconv.ParseBool(val)
		if err != nil {
			// If we cannot parse, default to enabled
			return true
		}
		return enabled
	}
)

var (
	// TODO: define all other default ports here, add docs

	// DefaultPortHTTPProxy is used as for HTTP PROXY mode. Can be overridden by ProxyHttpPort in mesh config.
	DefaultPortHTTPProxy = 15002
)
