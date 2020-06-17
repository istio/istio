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

package server

import (
	"testing"

	"istio.io/istio/mixer/pkg/config/store"
)

func TestValidation(t *testing.T) {
	a := DefaultArgs()

	if err := a.validate(); err != nil {
		t.Errorf("Expecting to validate but failed with: %v", err)
	}

	a = DefaultArgs()
	a.MaxMessageSize = 0
	if err := a.validate(); err == nil {
		t.Errorf("Got unexpected success")
	}

	a = DefaultArgs()
	a.MaxConcurrentStreams = 0
	if err := a.validate(); err == nil {
		t.Errorf("Got unexpected success")
	}

	a = DefaultArgs()
	a.AdapterWorkerPoolSize = -1
	if err := a.validate(); err == nil {
		t.Errorf("Got unexpected success")
	}

	a = DefaultArgs()
	a.APIWorkerPoolSize = -1
	if err := a.validate(); err == nil {
		t.Errorf("Got unexpected success")
	}

	a = DefaultArgs()
	a.NumCheckCacheEntries = -1
	if err := a.validate(); err == nil {
		t.Errorf("Got unexpected success")
	}

	a = DefaultArgs()
	a.ConfigStore = store.WithBackend(nil)
	a.ConfigStoreURL = "k8s://"
	if err := a.validate(); err == nil {
		t.Errorf("Got unexpected success")
	}
}

func TestString(t *testing.T) {
	a := DefaultArgs()

	// just make sure this doesn't crash
	s := a.String()
	t.Log(s)
}
