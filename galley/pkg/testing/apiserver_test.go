// Copyright 2018 Istio Authors
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

package testing

import (
	"testing"
)

func TestApiServer(t *testing.T) {
	a, err := newAPIServer()
	if err != nil {
		t.Fatalf("newAPIServer error: %v", err)
	}

	if err = a.close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
}

func TestSingleton(t *testing.T) {
	cfg := GetBaseAPIServerConfig()
	if cfg != nil {
		t.Fatalf("non-nil base config before init")
	}

	if err := InitAPIServer(); err != nil {
		t.Fatalf("InitAPIServer error: %v", err)
	}

	if cfg = GetBaseAPIServerConfig(); cfg == nil {
		t.Fatalf("nil base config after init")
	}

	if err := ShutdownAPIServer(); err != nil {
		t.Fatalf("ShutdownAPIServer error: %v", err)
	}

	if cfg = GetBaseAPIServerConfig(); cfg != nil {
		t.Fatalf("non-nil base config after shutdown")
	}
}

func TestSingleton_DoubleInit(t *testing.T) {
	if err := InitAPIServer(); err != nil {
		t.Fatalf("InitAPIServer error: %v", err)
	}

	if err := InitAPIServer(); err != nil {
		t.Fatalf("InitAPIServer error during second invocation: %v", err)
	}

	if err := ShutdownAPIServer(); err != nil {
		t.Fatalf("ShutdownAPIServer error: %v", err)
	}

	// use config as a way to check for shutdown detection.
	if cfg := GetBaseAPIServerConfig(); cfg != nil {
		t.Fatalf("non-nil base config after shutdown")
	}
}

func TestSingleton_DoubleShutdown(t *testing.T) {
	if err := InitAPIServer(); err != nil {
		t.Fatalf("InitAPIServer error: %v", err)
	}

	if err := ShutdownAPIServer(); err != nil {
		t.Fatalf("ShutdownAPIServer error: %v", err)
	}

	if err := ShutdownAPIServer(); err != nil {
		t.Fatalf("ShutdownAPIServer error during second invocation: %v", err)
	}
}
