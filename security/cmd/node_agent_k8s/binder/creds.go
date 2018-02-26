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

// Create Grpc Credentials
package binder

import (
	"errors"
	"net"
	"sync"

	"golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
)

type workloadStore struct {
	mu    sync.RWMutex
	creds map[string]workload
}

func newWorkloadStore() *workloadStore {
	return &workloadStore{creds: make(map[string]workload)}
}

func (s *workloadStore) ClientHandshake(_ context.Context, _ string, conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return conn, Credentials{}, errors.New("client handshake unsupported")
}

func (s *workloadStore) ServerHandshake(conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// TODO: maybe index this for faster lookup?
	addr := conn.LocalAddr()
	for _, w := range s.creds {
		if addrEqual(addr, w.listener.Addr()) {
			return conn, w.creds, nil
		}
	}

	return conn, Credentials{}, errors.New("unknown listener")
}

func (s *workloadStore) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{
		SecurityProtocol: authType,
		SecurityVersion:  "0.1",
		ServerName:       "workloadhandler",
	}
}

func (s *workloadStore) Clone() credentials.TransportCredentials {
	other := make(map[string]workload)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k, v := range s.creds {
		other[k] = v
	}
	return &workloadStore{creds: other}
}

func (s *workloadStore) OverrideServerName(_ string) error {
	return nil
}

// Internal methods for concurrent access to store data.

func (s *workloadStore) getAll() []workload {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ws := make([]workload, 0, len(s.creds))
	for _, w := range s.creds {
		ws = append(ws, w)
	}
	return ws
}

func (s *workloadStore) get(uid string) workload {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.creds[uid]
}

func (s *workloadStore) delete(uid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.creds, uid)
	return
}

func (s *workloadStore) store(uid string, w workload) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creds[uid] = w
}

func addrEqual(this, that net.Addr) bool {
	return this.Network() == that.Network() && this.String() == that.String()
}
