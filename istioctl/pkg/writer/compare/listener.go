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
	"fmt"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"

	"istio.io/istio/pkg/util/protomarshal"
)

// ListenerDiff prints a diff between Istiod and Envoy listeners to the passed writer
func (c *Comparator) ListenerDiff() error {
	envoyDump, err := c.envoy.GetDynamicListenerDump(true)
	if err != nil {
		return err
	}
	istiodDump, err := c.istiod.GetDynamicListenerDump(true)
	if err != nil {
		return err
	}
	if !proto.Equal(envoyDump, istiodDump) {
		// If not equal, marshal both dumps to JSON for diffing
		envoyJSON, err := protomarshal.ToJSONWithAnyResolver(envoyDump, "    ", &envoyResolver)
		if err != nil {
			return fmt.Errorf("error marshaling Envoy dump to JSON: %w", err)
		}

		istiodJSON, err := protomarshal.ToJSONWithAnyResolver(istiodDump, "    ", &envoyResolver)
		if err != nil {
			return fmt.Errorf("error marshaling Istiod dump to JSON: %w", err)
		}

		// Generate and print the diff
		diff := cmp.Diff(istiodJSON, envoyJSON)
		_, _ = fmt.Fprintln(c.w, "Listeners Don't Match. Diff:")
		_, _ = fmt.Fprintln(c.w, diff)
	} else {
		_, _ = fmt.Fprintln(c.w, "Listeners Match")
	}
	return nil
}
