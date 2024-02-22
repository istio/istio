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
	"istio.io/istio/pkg/env"
)

// Define flags that affect XDS config generation here.
var (
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
)
