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

package compare

import (
	"encoding/json"
	"fmt"
	"io"

	"istio.io/istio/istioctl/pkg/util/configdump"
)

// Comparator diffs between a config dump from Pilot and one from Envoy
type Comparator struct {
	envoy, pilot *configdump.Wrapper
	w            io.Writer
	context      int
}

// NewComparator is a comparator constructor
func NewComparator(w io.Writer, pilotResponses map[string][]byte, envoyResponse []byte) (*Comparator, error) {
	c := &Comparator{}
	for _, resp := range pilotResponses {
		pilotDump := &configdump.Wrapper{}
		err := json.Unmarshal(resp, pilotDump)
		if err != nil {
			continue
		}
		c.pilot = pilotDump
		break
	}
	if c.pilot == nil {
		return nil, fmt.Errorf("unable to find config dump in Pilot responses")
	}
	envoyDump := &configdump.Wrapper{}
	err := json.Unmarshal(envoyResponse, envoyDump)
	if err != nil {
		return nil, err
	}
	c.envoy = envoyDump
	c.w = w
	c.context = 7
	return c, nil
}

// Diff prints a diff between Pilot and Envoy to the passed writer
func (c *Comparator) Diff() error {
	if err := c.ClusterDiff(); err != nil {
		return err
	}
	if err := c.ListenerDiff(); err != nil {
		return err
	}
	return c.RouteDiff()
}
