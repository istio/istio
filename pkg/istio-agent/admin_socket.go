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
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// prepareAdminSocket requires a private directory owned by the effective Envoy identity.
// Only an existing socket may be removed on restart; symlinks and other files fail closed.
func prepareAdminSocket(socket string, uid, gid int) error {
	dir := filepath.Dir(socket)
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("admin directory %s is not a directory", dir)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot inspect admin directory ownership")
	}
	if int(stat.Uid) != uid || int(stat.Gid) != gid {
		if int(stat.Uid) != uid && os.Geteuid() != 0 {
			return fmt.Errorf("admin directory %s must be owned by %d:%d", dir, uid, gid)
		}
		if err := os.Chown(dir, uid, gid); err != nil {
			return err
		}
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	info, err = os.Lstat(socket)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket admin path %s", socket)
	}
	stat, ok = info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != uid {
		return fmt.Errorf("admin socket has unexpected owner")
	}
	conn, dialErr := net.DialTimeout("unix", socket, time.Second)
	if dialErr == nil {
		conn.Close()
		return fmt.Errorf("admin socket is already in use")
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) {
		return fmt.Errorf("cannot verify stale admin socket: %w", dialErr)
	}
	return os.Remove(socket)
}
