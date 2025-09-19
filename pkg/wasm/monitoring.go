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

package wasm

import "istio.io/istio/pkg/monitoring"

// Const strings for label value.
const (
	// For remote fetch metric.
	fetchSuccess     = "success"
	downloadFailure  = "download_failure"
	manifestFailure  = "manifest_failure"
	checksumMismatch = "checksum_mismatched"

	// For Wasm conversion metric.
	conversionSuccess   = "success"
	noRemoteLoad        = "no_remote_load"
	marshalFailure      = "marshal_failure"
	unmarshalFailure    = "unmarshal_failure"
	fetchFailure        = "fetch_failure"
	missRemoteFetchHint = "miss_remote_fetch_hint"
)

var (
	hitTag    = monitoring.CreateLabel("hit")
	resultTag = monitoring.CreateLabel("result")

	wasmCacheEntries = monitoring.NewGauge(
		"wasm_cache_entries",
		"number of Wasm remote fetch cache entries.",
	)

	wasmCacheLookupCount = monitoring.NewSum(
		"wasm_cache_lookup_count",
		"number of Wasm remote fetch cache lookups.",
	)

	wasmRemoteFetchCount = monitoring.NewSum(
		"wasm_remote_fetch_count",
		"number of Wasm remote fetches and results, including success, download failure, and checksum mismatch.",
	)

	wasmConfigConversionCount = monitoring.NewSum(
		"wasm_config_conversion_count",
		"number of Wasm config conversions and results, including success, no remote load, marshal failure, remote fetch failure, miss remote fetch hint.",
	)

	wasmConfigConversionDuration = monitoring.NewDistribution(
		"wasm_config_conversion_duration",
		"Total time in milliseconds istio-agent spends on converting remote load in Wasm config.",
		[]float64{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384},
	)
)
