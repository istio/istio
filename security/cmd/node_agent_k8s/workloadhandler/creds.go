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

package workloadhandler

import (
	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
)

func (s *Server) ClientHandshake(_ context.Context, _ string, conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	info := CredInfo{Err: ErrInvalidConnection}
	return conn, info, nil
}

func (s *Server) ServerHandshake(conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	var creds CredInfo

	if s.creds == nil {
		creds = CredInfo{Err: ErrNoCredentials}
	} else {
		creds = *s.creds
	}
	return conn, creds, nil
}

func (s *Server) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{
		SecurityProtocol: authType,
		SecurityVersion:  "0.1",
		ServerName:       "workloadhandler",
	}
}

func (s *Server) Clone() credentials.TransportCredentials {
	return &(*s)
}

func (s *Server) OverrideServerName(_ string) error {
	return nil
}

func (s *Server) GetCred() credentials.TransportCredentials {
	return s
}
