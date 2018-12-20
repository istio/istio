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

package ctrlz

import (
	"testing"
	"time"
)

func TestStartStop(t *testing.T) {
	t.Run("Enabled", func(t *testing.T) {
		done := make(chan struct{})
		listeningTestProbe = func() { close(done) }
		defer func() { listeningTestProbe = nil }()
		o := DefaultOptions()
		// TODO: Pick an unused port in o.Port.
		s, err := Run(o, nil)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		defer s.Close()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("Timed out waiting for listeningTestProbe to be called")
		}
	})

	t.Run("Disabled", func(t *testing.T) {
		listeningTestProbe = nil
		o := DefaultOptions()
		o.Port = 0
		s, err := Run(o, nil)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		s.Close()
	})
}
