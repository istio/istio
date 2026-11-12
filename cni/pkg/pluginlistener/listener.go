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

package pluginlistener

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"istio.io/istio/pkg/log"
)

// Listener is a unix socket listener that removes its socket file on Close only while
// the path still holds the socket it bound. When a node agent pod is replaced while the
// old one is still shutting down, the new agent has already bound its own socket at the
// same path, and the old agent must not unlink it.
type Listener struct {
	*net.UnixListener
	path  string
	bound os.FileInfo
}

// Listen removes any socket left at path and listens on it.
func Listen(network, path string) (*Listener, error) {
	// Remove unix socket before use.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		// Anything other than "file not found" is an error.
		return nil, fmt.Errorf("failed to remove unix://%s: %v", path, err)
	}

	addr, err := net.ResolveUnixAddr(network, path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve unix addr %q: %v", path, err)
	}
	ul, err := net.ListenUnix(network, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on unix socket %q: %v", path, err)
	}
	if strings.HasPrefix(path, "@") {
		// An abstract socket has no file to remove.
		return &Listener{UnixListener: ul, path: path}, nil
	}
	// Close removes the file itself, and only if it is still ours.
	ul.SetUnlinkOnClose(false)

	bound, err := os.Stat(path)
	if err != nil {
		_ = ul.Close()
		return nil, fmt.Errorf("unix socket %q doesn't exist after listen: %v", path, err)
	}
	return &Listener{UnixListener: ul, path: path, bound: bound}, nil
}

// Close closes the listener and removes its socket file unless another process has
// since bound a socket at the same path.
func (l *Listener) Close() error {
	err := l.UnixListener.Close()
	if l.bound == nil {
		return err
	}
	if cur, statErr := os.Stat(l.path); statErr == nil && os.SameFile(l.bound, cur) {
		_ = os.Remove(l.path)
	}
	return err
}

func NewListener(path string) (*Listener, error) {
	// Attempt to create the folder in case it doesn't exist
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		// If we cannot create it, just warn here - we will fail later if there is a real error
		log.Warnf("Failed to create directory for %v: %v", path, err)
	}

	listener, err := Listen("unix", path)
	if err != nil {
		return nil, err
	}

	// Update file permission so that cni plugin has permission to access it.
	if err := os.Chmod(path, 0o666); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("failed to update %q permission", path)
	}

	return listener, nil
}
