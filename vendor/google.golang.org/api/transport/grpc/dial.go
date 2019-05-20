// Copyright 2015 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package grpc supports network connections to GRPC servers.
// This package is not intended for use by end developers. Use the
// google.golang.org/api/option package to configure API clients.
package grpc

import (
	"context"
	"errors"
	"log"

	"go.opencensus.io/plugin/ocgrpc"
	"google.golang.org/api/internal"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/oauth"
)

// Set at init time by dial_appengine.go. If nil, we're not on App Engine.
var appengineDialerHook func(context.Context) grpc.DialOption

// Dial returns a GRPC connection for use communicating with a Google cloud
// service, configured with the given ClientOptions.
func Dial(ctx context.Context, opts ...option.ClientOption) (*grpc.ClientConn, error) {
	return dial(ctx, false, opts)
}

// DialInsecure returns an insecure GRPC connection for use communicating
// with fake or mock Google cloud service implementations, such as emulators.
// The connection is configured with the given ClientOptions.
func DialInsecure(ctx context.Context, opts ...option.ClientOption) (*grpc.ClientConn, error) {
	return dial(ctx, true, opts)
}

func dial(ctx context.Context, insecure bool, opts []option.ClientOption) (*grpc.ClientConn, error) {
	var o internal.DialSettings
	for _, opt := range opts {
		opt.Apply(&o)
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if o.HTTPClient != nil {
		return nil, errors.New("unsupported HTTP client specified")
	}
	if o.GRPCConn != nil {
		return o.GRPCConn, nil
	}
	grpcOpts := []grpc.DialOption{
		grpc.WithWaitForHandshake(),
	}
	if insecure {
		grpcOpts = []grpc.DialOption{grpc.WithInsecure()}
	} else if !o.NoAuth {
		if o.APIKey != "" {
			log.Print("API keys are not supported for gRPC APIs. Remove the WithAPIKey option from your client-creating call.")
		}
		creds, err := internal.Creds(ctx, &o)
		if err != nil {
			return nil, err
		}
		grpcOpts = []grpc.DialOption{
			grpc.WithPerRPCCredentials(grpcTokenSource{
				TokenSource:   oauth.TokenSource{creds.TokenSource},
				quotaProject:  o.QuotaProject,
				requestReason: o.RequestReason,
			}),
			grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")),
		}
	}
	if appengineDialerHook != nil {
		// Use the Socket API on App Engine.
		grpcOpts = append(grpcOpts, appengineDialerHook(ctx))
	}
	// Add tracing, but before the other options, so that clients can override the
	// gRPC stats handler.
	// This assumes that gRPC options are processed in order, left to right.
	grpcOpts = addOCStatsHandler(grpcOpts)
	grpcOpts = append(grpcOpts, o.GRPCDialOpts...)
	if o.UserAgent != "" {
		grpcOpts = append(grpcOpts, grpc.WithUserAgent(o.UserAgent))
	}
	return grpc.DialContext(ctx, o.Endpoint, grpcOpts...)
}

func addOCStatsHandler(opts []grpc.DialOption) []grpc.DialOption {
	return append(opts, grpc.WithStatsHandler(&ocgrpc.ClientHandler{}))
}

// grpcTokenSource supplies PerRPCCredentials from an oauth.TokenSource.
type grpcTokenSource struct {
	oauth.TokenSource

	// Additional metadata attached as headers.
	quotaProject  string
	requestReason string
}

// GetRequestMetadata gets the request metadata as a map from a grpcTokenSource.
func (ts grpcTokenSource) GetRequestMetadata(ctx context.Context, uri ...string) (
	map[string]string, error) {
	metadata, err := ts.TokenSource.GetRequestMetadata(ctx, uri...)
	if err != nil {
		return nil, err
	}

	// Attach system parameters into the metadata
	if ts.quotaProject != "" {
		metadata["X-goog-user-project"] = ts.quotaProject
	}
	if ts.requestReason != "" {
		metadata["X-goog-request-reason"] = ts.requestReason
	}
	return metadata, nil
}
