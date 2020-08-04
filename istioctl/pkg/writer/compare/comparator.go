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

package compare

import (
	"encoding/json"
	"fmt"
	"io"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/istioctl/pkg/util/configdump"
)

// Comparator diffs between a config dump from Istiod and one from Envoy
type Comparator struct {
	envoy, istiod *configdump.Wrapper
	w             io.Writer
	context       int
	location      string
}

// NewComparator is a comparator constructor
func NewComparator(w io.Writer, istiodResponses map[string][]byte, envoyResponse []byte) (*Comparator, error) {
	c := &Comparator{}
	for _, resp := range istiodResponses {
		istiodDump := &configdump.Wrapper{}
		err := json.Unmarshal(resp, istiodDump)
		if err != nil {
			continue
		}
		c.istiod = istiodDump
		break
	}
	if c.istiod == nil {
		return nil, fmt.Errorf("unable to find config dump in Istiod responses")
	}
	envoyDump := &configdump.Wrapper{}
	err := json.Unmarshal(envoyResponse, envoyDump)
	if err != nil {
		return nil, err
	}
	c.envoy = envoyDump
	c.w = w
	c.context = 7
	c.location = "Local" // the time.Location for formatting time.Time instances
	return c, nil
}

// NewXdsComparator is a comparator constructor
func NewXdsComparator(w io.Writer, istiodResponses map[string]*xdsapi.DiscoveryResponse, envoyResponse []byte) (*Comparator, error) {
	c := &Comparator{}
	for _, resp := range istiodResponses {
		if len(resp.Resources) > 0 {
			c.istiod = &configdump.Wrapper{
				&adminapi.ConfigDump{
					Configs: resp.Resources,
				},
			}
			break
		}
	}
	if c.istiod == nil {
		return nil, fmt.Errorf("unable to find config dump in Istiod responses")
	}
	envoyDump := &configdump.Wrapper{}
	err := json.Unmarshal(envoyResponse, envoyDump)
	if err != nil {
		return nil, err
	}
	c.envoy = envoyDump
	c.w = w
	c.context = 7
	c.location = "Local" // the time.Location for formatting time.Time instances
	return c, nil
}

// Diff prints a diff between Istiod and Envoy to the passed writer
func (c *Comparator) Diff() error {
	if err := c.ClusterDiff(); err != nil {
		return err
	}
	if err := c.ListenerDiff(); err != nil {
		return err
	}
	return c.RouteDiff()
}
