//go:build linux

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

package istioagent

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestPrivilegeDropOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires Linux root to verify privilege-drop ownership")
	}
	socket := filepath.Join(t.TempDir(), "admin", "admin.sock")
	for _, id := range []int{1337, 2000} {
		if err := prepareAdminSocket(socket, id, id+1); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(filepath.Dir(socket))
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		if stat.Uid != uint32(id) || stat.Gid != uint32(id+1) || info.Mode().Perm() != 0o700 {
			t.Fatalf("unexpected directory: %v %d:%d", info.Mode(), stat.Uid, stat.Gid)
		}
	}
}
