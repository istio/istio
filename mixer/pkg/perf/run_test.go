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
	"fmt"
	"testing"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/template"
)

type mockB struct {
	fatalLog []string
}

var _ benchmark = &mockB{}

func (b *mockB) name() string                            { return "mockB" }
func (b *mockB) n() int                                  { return 1 }
func (b *mockB) logf(format string, args ...interface{}) {}

func (b *mockB) run(name string, fn func(benchmark)) {
	fn(b)
}

func (b *mockB) fatalf(format string, args ...interface{}) {
	if b.fatalLog == nil {
		b.fatalLog = []string{}
	}
	b.fatalLog = append(b.fatalLog, fmt.Sprintf(format, args...))
}

var settings = Settings{
	RunMode:   InProcess,
	Templates: make(map[string]template.Info),
	Adapters:  []adapter.InfoFn{},
}

// TestBasic smoke-tests the basic run infrastructure. It is not a real benchmark.
func TestBasic(t *testing.T) {
	b := mockB{}
	run(&b, &MinimalSetup, &settings, false)

	if b.fatalLog != nil {
		t.Fatalf("error encountered during test: %v", b.fatalLog)
	}
}

// TestRunDispatcher smoke-tests the basic runDispatcherOnly infrastructure. It is not a real benchmark.
func TestRunDispatcher(t *testing.T) {
	b := mockB{}
	runDispatcherOnly(&b, &MinimalSetup, &settings)
}
