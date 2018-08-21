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
	"bytes"
	"fmt"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/pmezard/go-difflib/difflib"
)

// RouteDiff prints a diff between Pilot and Envoy routes to the passed writer
func (c *Comparator) RouteDiff() error {
	jsonm := &jsonpb.Marshaler{Indent: "   "}
	envoyBytes, pilotBytes := &bytes.Buffer{}, &bytes.Buffer{}
	envoyRouteDump, err := c.envoy.GetDynamicRouteDump(true)
	if err != nil {
		envoyBytes.WriteString(err.Error())
	} else if err := jsonm.Marshal(envoyBytes, envoyRouteDump); err != nil {
		return err
	}
	pilotRouteDump, err := c.pilot.GetDynamicRouteDump(true)
	if err != nil {
		pilotBytes.WriteString(err.Error())
	} else if err := jsonm.Marshal(pilotBytes, pilotRouteDump); err != nil {
		return err
	}
	diff := difflib.UnifiedDiff{
		FromFile: "Pilot Routes",
		A:        difflib.SplitLines(pilotBytes.String()),
		ToFile:   "Envoy Routes",
		B:        difflib.SplitLines(envoyBytes.String()),
		Context:  c.context,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return err
	}
	if text != "" {
		fmt.Fprintln(c.w, text)
	} else {
		fmt.Fprintln(c.w, "Routes Match")
	}
	return nil
}
