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
		SecurityVersion: "0.1",
		ServerName: "workloadhandler",
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
