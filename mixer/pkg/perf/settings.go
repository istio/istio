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

package perf

import (
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/template"
)

// RunMode configures the run mode for the perf.
type RunMode int

const (
	// InProcess run mode indicates that an in-process client should be used to drive the test.
	// This potentially affects collected profile data, as it will include client's execution as well.
	InProcess RunMode = iota

	// CoProcess run mode indicates that the client should be run in a separate process.
	// This avoids client's execution polluting the profile data, but may cause variations in timings
	// as the communication overhead between the client and server is counted in timings.
	CoProcess

	// InProcessBypassGrpc run mode indicates that the test should be run against the runtime.Dispatcher interface
	// directly. This is useful to reduce the scope that needs to be profiled, and allows discounting gRpc
	// and attribute preprocessing related overhead.
	InProcessBypassGrpc
)

// Settings is the perf test settings.
type Settings struct {
	RunMode   RunMode
	Templates map[string]template.Info
	Adapters  []adapter.InfoFn

	// ExecutableSearchSuffix indicates the search suffix to use when trying to locate the co-process
	// executable for perf testing. The process is located through an iterative search, starting with the
	// current working directory, and using the executablePathSuffix as the
	// search suffix for executable (e.g. "bazel-bin/mixer/test/perf/perfclient/perfclient").
	ExecutablePathSuffix string
}

func (s Settings) findTemplate(name string) (template.Info, bool) {
	t, f := s.Templates[name]
	return t, f
}

func (s Settings) findAdapter(name string) (adapter.InfoFn, bool) {
	for _, a := range s.Adapters {
		if a().Name == name {
			return a, true
		}
	}

	return nil, false
}
