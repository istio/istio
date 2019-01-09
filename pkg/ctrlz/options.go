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

package ctrlz

import (
	"strconv"

	"github.com/spf13/cobra"
)

// Options defines the set of options supported by Istio's ControlZ component introspection package.
type Options struct {
	// The IP port to use for ctrlz. Picked automatically if 0, in which case you can find out what
	// port was assigned by calling Server.Addr.
	Port uint16

	// The IP address to listen on for ctrlz.
	Address string

	// Whether CtrlZ is enabled.
	Enabled bool
}

// DefaultOptions returns a new set of options, initialized to the defaults
func DefaultOptions() *Options {
	return &Options{
		Port:    9876,
		Address: "127.0.0.1",
		Enabled: true,
	}
}

// Behaves like uint16 flag setting Port but also sets Enabled field accordingly.
type portFlag Options

// uint16-related code adapted from
// https://github.com/spf13/pflag/blob/master/uint16.go

func (p *portFlag) Set(s string) error {
	v, err := strconv.ParseUint(s, 0, 16)
	p.Port = uint16(v)
	p.Enabled = v != 0
	return err
}

func (p *portFlag) Type() string {
	return "uint16"
}

func (p *portFlag) String() string { return strconv.FormatUint(uint64(p.Port), 10) }

// AttachCobraFlags attaches a set of Cobra flags to the given Cobra command.
//
// Cobra is the command-line processor that Istio uses. This command attaches
// the necessary set of flags to expose a CLI to let the user control all
// introspection options.
func (o *Options) AttachCobraFlags(cmd *cobra.Command) {
	fs := cmd.PersistentFlags()
	fs.Var((*portFlag)(o), "ctrlz_port",
		"The IP port to use for the ControlZ introspection facility")
	fs.StringVar(&o.Address, "ctrlz_address", o.Address,
		"The IP Address to listen on for the ControlZ introspection facility. Use '*' to indicate all addresses.")
}
