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

package keepalive

import (
	"github.com/spf13/cobra"
	"time"
)

type Options struct {
	// After a duration of this time if the server/client doesn't see any activity it pings the peer to see if the transport is still alive.
	Time time.Duration
	// After having pinged for keepalive check, the server waits for a duration of Timeout and if no activity is seen even after that
	// the connection is closed.
	Timeout time.Duration
}

func DefaultOption() *Options {
	return &Options{
		Time:    30 * time.Second,
		Timeout: 10 * time.Second,
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
}
