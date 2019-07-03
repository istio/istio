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

// NewCtrlz returns a new ctrlz component.
func NewCtrlz(options *ctrlz.Options, topics ...fw.Topic) process.Component {

	var server *ctrlz.Server

	return process.ComponentFromFns(
		// start
		func() error {
			s, err := ctrlz.Run(options, topics)
			if err != nil {
				return err
			}

			server = s
			return nil
		},
		// stop
		func() {
			if server != nil {
				server.Close()
				server = nil
			}
		})
}
