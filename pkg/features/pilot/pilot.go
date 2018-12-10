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

	// AzDebug indicates whether to log service registry az info.
	AzDebug = os.Getenv("VERBOSE_AZ_DEBUG") == "1"

	// NetworkScopes isolates namespaces, limiting configuration for
	// egress and other mesh services to only hosts defined in same namespace or
	// 'admin' namespaces. Using services from any other namespaces will require the new NetworkScope
	// config. In most cases 'istio-system' should be included. Comma separated (ns1,ns2,istio-system)
	NetworkScopes = os.Getenv("DEFAULT_NAMESPACE_DEPENDENCIES")

	// SingleThreadPerNode eliminates 1/2 of the pilot go-routines and optimizes the push handling.
	// This is a temporary safety flag.
	SingleThreadPerNode = os.Getenv("THREAD_PER_NODE") == "1"

	// WaitAck waits for an ACK/NACK after pushing any config. If a nack is received the other configs will not
	// be pushed, but connection will not be closed. This may slow down the push for a node - the EDS will wait for
	// CDS to be acked for example, however it is safer.
	// This is a temporary safety flag.
	WaitAck = os.Getenv("WAIT_ACK") == "1"
)

const (

	// NodeMetadataNetwork defines the network the node belongs to. It is an optional metadata,
	// set at injection time. When set, the Endpoints returned to a note and not on same network
	// will be replaced with the gateway defined in the settings.
	NodeMetadataNetwork = "NETWORK"
)
