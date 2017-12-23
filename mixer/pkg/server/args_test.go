// Copyright 2017 Istio Authors
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

package server

import (
	"testing"
)

func TestValidation(t *testing.T) {
	a := NewArgs()

	if err := a.validate(); err != nil {
		t.Errorf("Expecting to validate but failed with: %v", err)
	}

	a.AdapterWorkerPoolSize = -1
	if err := a.validate(); err == nil {
		t.Errorf("Got unexpected success")
	}

	a = NewArgs()
	a.APIWorkerPoolSize = -1
	if err := a.validate(); err == nil {
		t.Errorf("Got unexpected success")
	}

	a = NewArgs()
	a.ExpressionEvalCacheSize = -1
	if err := a.validate(); err == nil {
		t.Errorf("Got unexpected success")
	}
}

func TestString(t *testing.T) {
	a := NewArgs()

	// just make sure this doesn't crash
	s := a.String()
	t.Log(s)
}

func TestEnableTracing(t *testing.T) {
	a := NewArgs()

	if a.EnableTracing() {
		t.Fatal("default arg values should not have enabled tracing")
	}

	a = NewArgs()
	a.LogTraceSpans = true
	if !a.EnableTracing() {
		t.Fatal("logTraceSpans should have trigged tracing")
	}

	a = NewArgs()
	a.ZipkinURL = "http://foo.bar.com"
	if !a.EnableTracing() {
		t.Fatal("zipkinURL should have trigged tracing")
	}

	a = NewArgs()
	a.JaegerURL = "http://foo.bar.com"
	if !a.EnableTracing() {
		t.Fatal("jaegerURL should have trigged tracing")
	}
}
