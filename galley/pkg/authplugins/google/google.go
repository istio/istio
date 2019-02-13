// Copyright 2019 Istio Authors
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

// Package google is a Galley auth plugin that uses Google application
// default credentials.
package google

import (
	"context"

	"golang.org/x/oauth2/google"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/oauth"

	"istio.io/istio/galley/pkg/authplugin"
)

// Default value for non-test
var findDC = google.FindDefaultCredentials

func returnAuth(map[string]string) ([]grpc.DialOption, error) {
	ctx := context.Background()
	creds, err := findDC(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, err
	}

	grpcOpts := []grpc.DialOption{
		grpc.WithPerRPCCredentials(oauth.TokenSource{TokenSource: creds.TokenSource}),
		grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")),
	}

	return grpcOpts, nil
}

func GetInfo() authplugin.Info {
	return authplugin.Info{
		Name:    "GOOGLE",
		GetAuth: returnAuth,
	}
}
