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
	"net"
	"os"
	"path/filepath"
	"testing"
)

func adminSocketDir(t *testing.T) string {
	t.Helper()
	// macOS Unix socket paths have a short length limit.
	dir, err := os.MkdirTemp("/tmp", "admin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestPrepareAdminSocket(t *testing.T) {
	for _, kind := range []string{"new", "restart", "file", "symlink", "directory", "live"} {
		t.Run(kind, func(t *testing.T) {
			parent := adminSocketDir(t)
			socket := filepath.Join(parent, "admin", "admin.sock")
			if err := prepareAdminSocket(socket, os.Getuid(), os.Getgid()); err != nil {
				t.Fatal(err)
			}
			info, _ := os.Stat(filepath.Dir(socket))
			if info.Mode().Perm() != 0o700 {
				t.Fatalf("mode %v", info.Mode())
			}
			switch kind {
			case "file":
				os.WriteFile(socket, nil, 0o600)
			case "symlink":
				os.Symlink(filepath.Join(parent, "target"), socket)
			case "directory":
				os.Mkdir(socket, 0o700)
			case "restart", "live":
				l, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
				if err != nil {
					t.Fatal(err)
				}
				l.SetUnlinkOnClose(false)
				if kind == "restart" {
					l.Close()
				} else {
					defer l.Close()
				}
			}
			err := prepareAdminSocket(socket, os.Getuid(), os.Getgid())
			fail := kind != "new" && kind != "restart"
			if (err != nil) != fail {
				t.Fatalf("got %v, want failure %v", err, fail)
			}
		})
	}
}

func TestUnsafeDirectory(t *testing.T) {
	for _, kind := range []string{"file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(adminSocketDir(t), "admin")
			if kind == "file" {
				if err := os.WriteFile(dir, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Symlink(adminSocketDir(t), dir); err != nil {
					t.Fatal(err)
				}
			}
			if err := prepareAdminSocket(filepath.Join(dir, "admin.sock"), os.Getuid(), os.Getgid()); err == nil {
				t.Fatal("accepted unsafe directory")
			}
		})
	}
}
