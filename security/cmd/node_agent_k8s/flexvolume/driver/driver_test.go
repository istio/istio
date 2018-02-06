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

func TestAttach(t *testing.T) {
	err := Attach("opt", "dummynode")
	if err != nil {
		t.Errorf("Failed to attach.")
	}
}

func TestDetach(t *testing.T) {
	err := Detach("device")
	if err != nil {
		t.Errorf("Failed to detach.")
	}
}

func TestWaitAttach(t *testing.T) {
	err := WaitAttach("device", "opts")
	if err != nil {
		t.Errorf("Failed to WaitAttach.")
	}
}

func TestIsAttached(t *testing.T) {
	err := IsAttached("device", "opts")
	if err != nil {
		t.Errorf("IsAttached function failed.")
	}
}

func TestMountDev(t *testing.T) {
	err := MountDev("device", "opts", "path")
	if err != nil {
		t.Errorf("MountDev function failed.")
	}
}

func TestUnmountDev(t *testing.T) {
	err := UnmountDev("device")
	if err != nil {
		t.Errorf("UnmountDev function failed.")
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

func TestGetVolName(t *testing.T) {
	err := GetVolName("/tmp")
	if err != nil {
		t.Errorf("GetVolName function failed.")
	}
}
