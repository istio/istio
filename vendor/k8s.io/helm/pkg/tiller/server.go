/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tiller

import (
	"fmt"
	"log"
	"strings"

	goprom "github.com/grpc-ecosystem/go-grpc-prometheus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"k8s.io/helm/pkg/version"
)

// maxMsgSize use 20MB as the default message size limit.
// grpc library default is 4MB
const maxMsgSize = 1024 * 1024 * 20

// DefaultServerOpts returns the set of default grpc ServerOption's that Tiller requires.
func DefaultServerOpts() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.MaxRecvMsgSize(maxMsgSize),
		grpc.MaxSendMsgSize(maxMsgSize),
		grpc.UnaryInterceptor(newUnaryInterceptor()),
		grpc.StreamInterceptor(newStreamInterceptor()),
	}
}

// NewServer creates a new grpc server.
func NewServer(opts ...grpc.ServerOption) *grpc.Server {
	return grpc.NewServer(append(DefaultServerOpts(), opts...)...)
}

func newUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		if err := checkClientVersion(ctx); err != nil {
			// whitelist GetVersion() from the version check
			if _, m := splitMethod(info.FullMethod); m != "GetVersion" {
				log.Println(err)
				return nil, err
			}
		}
		return goprom.UnaryServerInterceptor(ctx, req, info, handler)
	}
}

func newStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := checkClientVersion(ss.Context()); err != nil {
			log.Println(err)
			return err
		}
		return goprom.StreamServerInterceptor(srv, ss, info, handler)
	}
}

func splitMethod(fullMethod string) (string, string) {
	if frags := strings.Split(fullMethod, "/"); len(frags) == 3 {
		return frags[1], frags[2]
	}
	return "unknown", "unknown"
}

func versionFromContext(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v, ok := md["x-helm-api-client"]; ok && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

func checkClientVersion(ctx context.Context) error {
	clientVersion := versionFromContext(ctx)
	if !version.IsCompatible(clientVersion, version.GetVersion()) {
		return fmt.Errorf("incompatible versions client[%s] server[%s]", clientVersion, version.GetVersion())
	}
	return nil
}
