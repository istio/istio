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

package features

import (
	"time"

	"go.uber.org/atomic"

	"istio.io/istio/pkg/env"
)

// Define flags that affect XDS config generation here.
var (
	// FilterGatewayClusterConfig controls if a subset of clusters(only those required) should be pushed to gateways
	FilterGatewayClusterConfig = env.Register("PILOT_FILTER_GATEWAY_CLUSTER_CONFIG", false,
		"If enabled, Pilot will send only clusters that referenced in gateway virtual services attached to gateway").Get()

	SendUnhealthyEndpoints = atomic.NewBool(env.Register(
		"PILOT_SEND_UNHEALTHY_ENDPOINTS",
		false,
		"If enabled, Pilot will include unhealthy endpoints in EDS pushes and even if they are sent Envoy does not use them for load balancing."+
			"  To avoid, sending traffic to non ready endpoints, enabling this flag, disables panic threshold in Envoy i.e. Envoy does not load balance requests"+
			" to unhealthy/non-ready hosts even if the percentage of healthy hosts fall below minimum health percentage(panic threshold).",
	).Get())

	// EnableMysqlFilter enables injection of `envoy.filters.network.mysql_proxy` in the filter chain.
	// Pilot injects this outbound filter if the service port name is `mysql`.
	EnableMysqlFilter = env.Register(
		"PILOT_ENABLE_MYSQL_FILTER",
		false,
		"EnableMysqlFilter enables injection of `envoy.filters.network.mysql_proxy` in the filter chain.",
	).Get()

	// EnableRedisFilter enables injection of `envoy.filters.network.redis_proxy` in the filter chain.
	// Pilot injects this outbound filter if the service port name is `redis`.
	EnableRedisFilter = env.Register(
		"PILOT_ENABLE_REDIS_FILTER",
		false,
		"EnableRedisFilter enables injection of `envoy.filters.network.redis_proxy` in the filter chain.",
	).Get()

	// EnableMongoFilter enables injection of `envoy.filters.network.mongo_proxy` in the filter chain.
	EnableMongoFilter = env.Register(
		"PILOT_ENABLE_MONGO_FILTER",
		true,
		"EnableMongoFilter enables injection of `envoy.filters.network.mongo_proxy` in the filter chain.",
	).Get()

	// UseRemoteAddress sets useRemoteAddress to true for sidecar outbound listeners so that it picks up the localhost
	// address of the sender, which is an internal address, so that trusted headers are not sanitized.
	UseRemoteAddress = env.Register(
		"PILOT_SIDECAR_USE_REMOTE_ADDRESS",
		false,
		"UseRemoteAddress sets useRemoteAddress to true for sidecar outbound listeners.",
	).Get()

	EnableHeadlessService = env.Register(
		"PILOT_ENABLE_HEADLESS_SERVICE_POD_LISTENERS",
		true,
		"If enabled, for a headless service/stateful set in Kubernetes, pilot will generate an "+
			"outbound listener for each pod in a headless service. This feature should be disabled "+
			"if headless services have a large number of pods.",
	).Get()

	EnableEDSForHeadless = env.Register(
		"PILOT_ENABLE_EDS_FOR_HEADLESS_SERVICES",
		false,
		"If enabled, for headless service in Kubernetes, pilot will send endpoints over EDS, "+
			"allowing the sidecar to load balance among pods in the headless service. This feature "+
			"should be enabled if applications access all services explicitly via a HTTP proxy port in the sidecar.",
	).Get()

	EnableXDSCaching = env.Register("PILOT_ENABLE_XDS_CACHE", true,
		"If true, Pilot will cache XDS responses.").Get()

	// EnableCDSCaching determines if CDS caching is enabled. This is explicitly split out of ENABLE_XDS_CACHE,
	// so that in case there are issues with the CDS cache we can just disable the CDS cache.
	EnableCDSCaching = env.Register("PILOT_ENABLE_CDS_CACHE", true,
		"If true, Pilot will cache CDS responses. Note: this depends on PILOT_ENABLE_XDS_CACHE.").Get()

	// EnableRDSCaching determines if RDS caching is enabled. This is explicitly split out of ENABLE_XDS_CACHE,
	// so that in case there are issues with the RDS cache we can just disable the RDS cache.
	EnableRDSCaching = env.Register("PILOT_ENABLE_RDS_CACHE", true,
		"If true, Pilot will cache RDS responses. Note: this depends on PILOT_ENABLE_XDS_CACHE.").Get()

	EnableXDSCacheMetrics = env.Register("PILOT_XDS_CACHE_STATS", false,
		"If true, Pilot will collect metrics for XDS cache efficiency.").Get()

	XDSCacheMaxSize = env.Register("PILOT_XDS_CACHE_SIZE", 60000,
		"The maximum number of cache entries for the XDS cache.").Get()

	XDSCacheIndexClearInterval = env.Register("PILOT_XDS_CACHE_INDEX_CLEAR_INTERVAL", 5*time.Second,
		"The interval for xds cache index clearing.").Get()

	XdsPushSendTimeout = env.Register(
		"PILOT_XDS_SEND_TIMEOUT",
		0*time.Second,
		"The timeout to send the XDS configuration to proxies. After this timeout is reached, Pilot will discard that push.",
	).Get()
)
