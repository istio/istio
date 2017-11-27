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

package perf

import (
	"testing"
)

func TestControllerBasic(t *testing.T) {
	c, err := newController()
	if err != nil {
		t.Fatalf("Error creating controller: %v", err)
	}

	s, err := NewClientServer(c.location())
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	c.waitForClient()

	err = c.initializeClients("10.10.10.10", &Setup{})
	if err != nil {
		t.Fatalf("Initialization failed: %v", err)
	}
	err = c.runClients(10)
	if err != nil {
		t.Fatalf("run failed.")
	}
	err = c.runClients(50)
	if err != nil {
		t.Fatalf("run failed")
	}
	c.close()

	s.Wait()
}

func TestAgent_NoController(t *testing.T) {
	l := ServiceLocation{Address: "127.0.0.1:34829", Path: "/foo"}
	_, err := NewClientServer(l)
	if err == nil {
		t.Fail()
	}
}
