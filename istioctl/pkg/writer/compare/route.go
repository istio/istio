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
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"

	"istio.io/istio/pkg/util/protomarshal"
)

// RouteDiff prints a diff between Istiod and Envoy routes to the passed writer
func (c *Comparator) RouteDiff() error {
	envoyDump, err := c.envoy.GetDynamicRouteDump(true)
	if err != nil {
		return err
	}
	istiodDump, err := c.istiod.GetDynamicRouteDump(true)
	if err != nil {
		return err
	}
	lastUpdatedStr := ""
	if lastUpdated, err := c.envoy.GetLastUpdatedDynamicRouteTime(); err != nil {
		return err
	} else if lastUpdated != nil {
		loc, err := time.LoadLocation(c.location)
		if err != nil {
			loc, _ = time.LoadLocation("UTC")
		}
		lastUpdatedStr = fmt.Sprintf(" (RDS last loaded at %s)", lastUpdated.In(loc).Format(time.RFC1123))
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
		_, _ = fmt.Fprintf(c.w, "Routes Don't Match%s\n", lastUpdatedStr)
		_, _ = fmt.Fprintln(c.w, diff)
	} else {
		_, _ = fmt.Fprintf(c.w, "Routes Match%s\n", lastUpdatedStr)
	}
	return nil
}
