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
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestCNIListener(t *testing.T) {
	f := filepath.Join(t.TempDir(), "test")
	l, err := NewListener(f)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	defer l.Close()
	conn, err := connect(f)
	if err != nil {
		t.Fatalf("failed to connect %v", err)
	}
	conn.Close()
}

func connect(socket string) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption

	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}))

	conn, err := grpc.Dial(socket, opts...)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

func TestCloseRemovesOwnSocket(t *testing.T) {
	f := filepath.Join(t.TempDir(), "test")
	l, err := NewListener(f)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatalf("expected the socket file removed, stat err: %v", err)
	}
}

// A replacement agent binds its own socket at the path while the old agent is still
// shutting down; the old agent's Close must leave the new socket in place.
func TestCloseLeavesReplacedSocket(t *testing.T) {
	for _, network := range []string{"unix", "unixpacket"} {
		t.Run(network, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "test")
			old, err := Listen(network, f)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			replacement, err := Listen(network, f)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			defer replacement.Close()

			if err := old.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			if _, err := os.Stat(f); err != nil {
				t.Fatalf("the replacement's socket was removed: %v", err)
			}
			conn, err := net.Dial(network, f)
			if err != nil {
				t.Fatalf("the replacement's socket does not accept connections: %v", err)
			}
			conn.Close()

			if err := replacement.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			if _, err := os.Stat(f); !os.IsNotExist(err) {
				t.Fatalf("expected the socket file removed, stat err: %v", err)
			}
		})
	}
}
