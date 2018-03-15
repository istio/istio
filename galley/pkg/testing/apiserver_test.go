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
	a, err := newApiServer()
	if err != nil {
		t.Fatalf("newApiServer error: %v", err)
	}

	if err = a.close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
}

func TestSingleton(t *testing.T) {
	cfg := GetBaseApiServerConfig()
	if cfg != nil {
		t.Fatalf("non-nil base config before init")
	}

	if err := InitApiServer(); err != nil {
		t.Fatalf("InitApiServer error: %v", err)
	}

	if cfg = GetBaseApiServerConfig(); cfg == nil {
		t.Fatalf("nil base config after init")
	}

	if err := ShutdownApiServer(); err != nil {
		t.Fatalf("ShutdownApiServer error: %v", err)
	}

	if cfg = GetBaseApiServerConfig(); cfg != nil {
		t.Fatalf("non-nil base config after shutdown")
	}
}

func TestSingleton_DoubleInit(t *testing.T) {
	if err := InitApiServer(); err != nil {
		t.Fatalf("InitApiServer error: %v", err)
	}

	if err := InitApiServer(); err != nil {
		t.Fatalf("InitApiServer error during second invocation: %v", err)
	}

	if err := ShutdownApiServer(); err != nil {
		t.Fatalf("ShutdownApiServer error: %v", err)
	}

	// use config as a way to check for shutdown detection.
	if cfg := GetBaseApiServerConfig(); cfg != nil {
		t.Fatalf("non-nil base config after shutdown")
	}
}


func TestSingleton_DoubleShutdown(t *testing.T) {
	if err := InitApiServer(); err != nil {
		t.Fatalf("InitApiServer error: %v", err)
	}

	if err := ShutdownApiServer(); err != nil {
		t.Fatalf("ShutdownApiServer error: %v", err)
	}

	if err := ShutdownApiServer(); err != nil {
		t.Fatalf("ShutdownApiServer error during second invocation: %v", err)
	}
}
