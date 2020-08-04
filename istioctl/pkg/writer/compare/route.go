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
	"bytes"
	"fmt"
	"time"

	"github.com/golang/protobuf/jsonpb"
	"github.com/pmezard/go-difflib/difflib"
)

// RouteDiff prints a diff between Istiod and Envoy routes to the passed writer
func (c *Comparator) RouteDiff() error {
	jsonm := &jsonpb.Marshaler{Indent: "   "}
	envoyBytes, istiodBytes := &bytes.Buffer{}, &bytes.Buffer{}
	envoyRouteDump, err := c.envoy.GetDynamicRouteDump(true)
	if err != nil {
		envoyBytes.WriteString(err.Error())
	} else if err := jsonm.Marshal(envoyBytes, envoyRouteDump); err != nil {
		return err
	}
	istiodRouteDump, err := c.istiod.GetDynamicRouteDump(true)
	if err != nil {
		istiodBytes.WriteString(err.Error())
	} else if err := jsonm.Marshal(istiodBytes, istiodRouteDump); err != nil {
		return err
	}
	diff := difflib.UnifiedDiff{
		FromFile: "Istiod Routes",
		A:        difflib.SplitLines(istiodBytes.String()),
		ToFile:   "Envoy Routes",
		B:        difflib.SplitLines(envoyBytes.String()),
		Context:  c.context,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
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
	if text != "" {
		fmt.Fprintf(c.w, "Routes Don't Match%s\n", lastUpdatedStr)
		fmt.Fprintln(c.w, text)
	} else {
		fmt.Fprintf(c.w, "Routes Match%s\n", lastUpdatedStr)
	}
	return nil
}
