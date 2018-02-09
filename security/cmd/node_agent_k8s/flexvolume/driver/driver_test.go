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

package driver

import (
	"testing"
)

func TestInit(t *testing.T) {
	ver := "1.8"
	err := Init(ver)
	if err != nil {
		t.Errorf("Failed to init.")
	}

	ver = "1.9"
	err = Init(ver)
	if err != nil {
		t.Errorf("Failed to init.")
	}
}

// TODO(wattli): improve the testing cases for Mount().
func TestMount(t *testing.T) {
	err := Mount("device", "opts")
	if err != nil {
		t.Errorf("Mount function failed.")
	}

	opts := `{"Uid": "myuid", "Nme": "myname", "Namespace": "mynamespace", "ServiceAccount": "myaccount"}`
	err = Mount("/tmp", opts)
	if err != nil {
		t.Errorf("Mount function failed: %s.", err.Error())
	}
}

func TestUnmount(t *testing.T) {
	err := Unmount("/tmp")
	if err != nil {
		t.Errorf("Unmount function failed.")
	}
}
