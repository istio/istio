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

package grpc

import (
	"math"
	"time"

	"github.com/spf13/cobra"
)

const (
	// Infinity is the maximum possible duration for keepalive values
	Infinity = time.Duration(math.MaxInt64)
)

// TODO: Move other grpc options here, if necessary
// Options defines the set of options used for grpc.
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
	// WriteBufferSize determines how much data can be batched before doing a write on the wire.
	// The corresponding memory allocation for this buffer will be twice the size to keep syscalls low.
	// The default value for this buffer is 32KB.
	WriteBufferSize int
	// ReadBufferSize lets you set the size of read buffer, this determines how much data can be read at most
	// for one read syscall.
	// The default value for this buffer is 32KB.
	ReadBufferSize int
}

// DefaultOption returns the default grpc options.
func DefaultOption() *Options {
	return &Options{
		Time:                        30 * time.Second,
		Timeout:                     10 * time.Second,
		MaxServerConnectionAge:      Infinity,
		MaxServerConnectionAgeGrace: 10 * time.Second,
		WriteBufferSize:             32 * 1024,
		ReadBufferSize:              32 * 1024,
	}
}

// AttachCobraFlags attaches a set of Cobra flags to the given Cobra command.
//
// Cobra is the command-line processor that Istio uses. This command attaches
// the necessary set of flags to configure the grpc options.
func (o *Options) AttachCobraFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().DurationVar(&o.Time, "keepaliveInterval", o.Time,
		"The time interval if no activity on the connection it pings the peer to see if the transport is alive")
	cmd.PersistentFlags().DurationVar(&o.Timeout, "keepaliveTimeout", o.Timeout,
		"After having pinged for keepalive check, the client/server waits for a duration of keepaliveTimeout "+
			"and if no activity is seen even after that the connection is closed.")
	cmd.PersistentFlags().DurationVar(&o.MaxServerConnectionAge, "keepaliveMaxServerConnectionAge",
		o.MaxServerConnectionAge, "Maximum duration a connection will be kept open on the server before a graceful close.")
	cmd.PersistentFlags().IntVar(&o.WriteBufferSize, "writeBufferSize", o.WriteBufferSize,
		"WriteBufferSize determines how much data can be batched before doing a write on the wire. By increasing it, "+
			"you can slightly increase the performance of the pilot. Decrease/increase 1KB WriteBufferSize can "+
			"decrease/increase 2n KB memory consumption, n is the number of xds connections managed by pilot.")
	cmd.PersistentFlags().IntVar(&o.ReadBufferSize, "readBufferSize", o.ReadBufferSize,
		"ReadBufferSize lets you set the size of read buffer, this determines how much data can be read at "+
			"most for one read syscall. Decrease/increase 1KB ReadBufferSize can decrease/increase n KB memory consumption,"+
			"n is the number of xds connections managed by pilot.")
}
