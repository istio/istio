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

package keepalive

import (
	"math"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"istio.io/istio/pkg/env"
)

const (
	// Infinity is the maximum possible duration for keepalive values
	Infinity = time.Duration(math.MaxInt64)
)

var (
	// grpcKeepaliveInterval sets the gRPC KeepAlive Interval
	grpcKeepaliveInterval = env.Register("GRPC_KEEPALIVE_INTERVAL", 30*time.Second, "gRPC Keepalive Interval").Get()

	// grpcKeepAliveTimeout sets the gRPC KeepAlive Timeout
	grpcKeepaliveTimeout = env.Register("GRPC_KEEPALIVE_TIMEOUT", 10*time.Second, "gRPC Keepalive Timeout").Get()
)

// Options defines the set of options used for grpc keepalive.
// The Time and Timeout options are used for both client and server connections,
// whereas MaxServerConnectionAge* options are applicable on the server side only
// (as implied by the options' name...)
type Options struct {
	// After a duration of this time if the server/client doesn't see any activity it pings the peer to see if the transport is still alive.
	Time time.Duration
	// After having pinged for keepalive check, the server waits for a duration of Timeout and if no activity is seen even after that
	// the connection is closed.
	Timeout time.Duration
	// MaxServerConnectionAge is a duration for the maximum amount of time a
	// connection may exist before it will be closed by the server sending a GoAway.
	// A random jitter is added to spread out connection storms.
	// See https://github.com/grpc/grpc-go/blob/bd0b3b2aa2a9c87b323ee812359b0e9cda680dad/keepalive/keepalive.go#L49
	MaxServerConnectionAge time.Duration // default value is infinity
	// MaxServerConnectionAgeGrace is an additive period after MaxServerConnectionAge
	// after which the connection will be forcibly closed by the server.
	MaxServerConnectionAgeGrace time.Duration // default value 10s
}

// ConvertToClientOption convert Options to grpc client option used for grpc keepalive.
func (o *Options) ConvertToClientOption() grpc.DialOption {
	return grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:    o.Time,
		Timeout: o.Timeout,
	})
}

// DefaultOption returns the default keepalive options.
func DefaultOption() *Options {
	return &Options{
		Time:                        grpcKeepaliveInterval,
		Timeout:                     grpcKeepaliveTimeout,
		MaxServerConnectionAge:      Infinity,
		MaxServerConnectionAgeGrace: 10 * time.Second,
	}
}

// AttachCobraFlags attaches a set of Cobra flags to the given Cobra command.
//
// Cobra is the command-line processor that Istio uses. This command attaches
// the necessary set of flags to configure the grpc keepalive options.
func (o *Options) AttachCobraFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().DurationVar(&o.Time, "keepaliveInterval", o.Time,
		"The time interval if no activity on the connection it pings the peer to see if the transport is alive")
	cmd.PersistentFlags().DurationVar(&o.Timeout, "keepaliveTimeout", o.Timeout,
		"After having pinged for keepalive check, the client/server waits for a duration of keepaliveTimeout "+
			"and if no activity is seen even after that the connection is closed.")
	cmd.PersistentFlags().DurationVar(&o.MaxServerConnectionAge, "keepaliveMaxServerConnectionAge",
		o.MaxServerConnectionAge, "Maximum duration a connection will be kept open on the server before a graceful close.")
}
