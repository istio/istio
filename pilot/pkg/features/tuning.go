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
	"runtime"
	"time"

	"istio.io/istio/pkg/env"
)

// Define performance tuning related features here.
var (
	MaxConcurrentStreams = env.Register(
		"ISTIO_GPRC_MAXSTREAMS",
		100000,
		"Sets the maximum number of concurrent grpc streams.",
	).Get()

	// MaxRecvMsgSize The max receive buffer size of gRPC received channel of Pilot in bytes.
	MaxRecvMsgSize = env.Register(
		"ISTIO_GPRC_MAXRECVMSGSIZE",
		4*1024*1024,
		"Sets the max receive buffer size of gRPC stream in bytes.",
	).Get()

	PushThrottle = func() int {
		v := env.Register(
			"PILOT_PUSH_THROTTLE",
			0,
			"Limits the number of concurrent pushes allowed. On larger machines this can be increased for faster pushes. "+
				"If set to 0 or unset, the max will be automatically determined based on the machine size",
		).Get()
		if v > 0 {
			return v
		}
		procs := runtime.GOMAXPROCS(0)
		// Heuristic to scale with cores. We end up with...
		// 1: 20
		// 2: 25
		// 4: 35
		// 32: 100
		return min(15+5*procs, 100)
	}()

	RequestLimit = func() float64 {
		v := env.Register(
			"PILOT_MAX_REQUESTS_PER_SECOND",
			0.0,
			"Limits the number of incoming XDS requests per second. On larger machines this can be increased to handle more proxies concurrently. "+
				"If set to 0 or unset, the max will be automatically determined based on the machine size",
		).Get()
		if v > 0 {
			return v
		}
		procs := runtime.GOMAXPROCS(0)
		// Heuristic to scale with cores. We end up with...
		// 1: 20
		// 2: 25
		// 4: 35
		// 32: 100
		return min(float64(15+5*procs), 100.0)
	}()

	DebounceAfter = env.Register(
		"PILOT_DEBOUNCE_AFTER",
		100*time.Millisecond,
		"The delay added to config/registry events for debouncing. This will delay the push by "+
			"at least this interval. If no change is detected within this period, the push will happen, "+
			" otherwise we'll keep delaying until things settle, up to a max of PILOT_DEBOUNCE_MAX.",
	).Get()

	DebounceMax = env.Register(
		"PILOT_DEBOUNCE_MAX",
		10*time.Second,
		"The maximum amount of time to wait for events while debouncing. If events keep showing up with no breaks "+
			"for this time, we'll trigger a push.",
	).Get()

	EnableEDSDebounce = env.Register(
		"PILOT_ENABLE_EDS_DEBOUNCE",
		true,
		"If enabled, Pilot will include EDS pushes in the push debouncing, configured by PILOT_DEBOUNCE_AFTER and PILOT_DEBOUNCE_MAX."+
			" EDS pushes may be delayed, but there will be fewer pushes. By default this is enabled",
	).Get()

	ConvertSidecarScopeConcurrency = env.Register(
		"PILOT_CONVERT_SIDECAR_SCOPE_CONCURRENCY",
		1,
		"Deprecated, superseded by ENABLE_LAZY_SIDECAR_EVALUATION. Used to adjust the concurrency of SidecarScope conversions. "+
			"When istiod is deployed on a multi-core CPU server, increasing this value will help to use the CPU to "+
			"accelerate configuration push, but it also means that istiod will consume more CPU resources.",
	).Get()

	MutexProfileFraction = env.Register("MUTEX_PROFILE_FRACTION", 1000,
		"If set to a non-zero value, enables mutex profiling a rate of 1/MUTEX_PROFILE_FRACTION events."+
			" For example, '1000' will record 0.1% of events. "+
			"Set to 0 to disable entirely.").Get()

	StatusUpdateInterval = env.Register(
		"PILOT_STATUS_UPDATE_INTERVAL",
		500*time.Millisecond,
		"Interval to update the XDS distribution status.",
	).Get()

	StatusQPS = env.Register(
		"PILOT_STATUS_QPS",
		100,
		"If status is enabled, controls the QPS with which status will be updated.  "+
			"See https://godoc.org/k8s.io/client-go/rest#Config QPS",
	).Get()

	StatusBurst = env.Register(
		"PILOT_STATUS_BURST",
		500,
		"If status is enabled, controls the Burst rate with which status will be updated.  "+
			"See https://godoc.org/k8s.io/client-go/rest#Config Burst",
	).Get()

	StatusMaxWorkers = env.Register("PILOT_STATUS_MAX_WORKERS", 100, "The maximum number of workers"+
		" Pilot will use to keep configuration status up to date.  Smaller numbers will result in higher status latency, "+
		"but larger numbers may impact CPU in high scale environments.").Get()

	XDSCacheMaxSize = env.Register("PILOT_XDS_CACHE_SIZE", 60000,
		"The maximum number of cache entries for the XDS cache.").Get()

	XDSCacheIndexClearInterval = env.Register("PILOT_XDS_CACHE_INDEX_CLEAR_INTERVAL", 5*time.Second,
		"The interval for xds cache index clearing.").Get()
)
