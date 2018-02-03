package workloadhandler

import (
	"errors"

	"golang.org/x/net/context"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

var (
	ErrInvalidConnection = errors.New("invalid connection")
	ErrNoCredentials = errors.New("No credentials available")
)

const (
	authType = "udsuspver"
)

// Information returned by grpc Credential that the workload API can use.
type CredInfo struct {
	Uid            string
	Name           string
	Namespace      string
	ServiceAccount string
	Err		error
}

func (c CredInfo) AuthType() string {
	return authType
}

func CallerFromContext(ctx context.Context) (CredInfo, bool) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		return CredInfo{}, false
	}
	return CallerFromAuthInfo(peer.AuthInfo)
}

func CallerFromAuthInfo(ainfo credentials.AuthInfo) (CredInfo, bool) {
	if ci, ok := ainfo.(CredInfo); ok {
		return ci, true
	}
	return CredInfo{}, false
}
