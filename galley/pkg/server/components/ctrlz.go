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

package components

import (
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/ctrlz/fw"

	"istio.io/istio/galley/pkg/server/process"
)

// Ctrlz component that starts/stops a Ctrlz service.
type Ctrlz struct {
	options *ctrlz.Options
	topics  []fw.Topic
	server  *ctrlz.Server
}

var _ process.Component = &Ctrlz{}

// NewCtrlz returns a new ctrlz component.
func NewCtrlz(options *ctrlz.Options, topics ...fw.Topic) *Ctrlz {
	return &Ctrlz{
		options: options,
		topics:  topics,
	}
}

// Start implements process.Component
func (c *Ctrlz) Start() error {
	s, err := ctrlz.Run(c.options, c.topics)
	if err != nil {
		return err
	}

	c.server = s
	return nil
}

// Stop implements process.Component
func (c *Ctrlz) Stop() {
	if c.server != nil {
		c.server.Close()
		c.server = nil
	}
}

// Address returns the local Ctrlz server address.
func (c *Ctrlz) Address() string {
	addr := ""
	if c.server != nil {
		addr = c.server.Address()
	}
	return addr
}
