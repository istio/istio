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

// ListenerDiff prints a diff between Pilot and Envoy listeners to the passed writer
func (c *Comparator) ListenerDiff() error {
	jsonm := &jsonpb.Marshaler{Indent: "   "}
	envoyBytes, pilotBytes := &bytes.Buffer{}, &bytes.Buffer{}
	envoyListenerDump, err := c.envoy.GetDynamicListenerDump(true)
	if err != nil {
		envoyBytes.WriteString(err.Error())
	} else if err := jsonm.Marshal(envoyBytes, envoyListenerDump); err != nil {
		return err
	}
	pilotListenerDump, err := c.pilot.GetDynamicListenerDump(true)
	if err != nil {
		pilotBytes.WriteString(err.Error())
	} else if err := jsonm.Marshal(pilotBytes, pilotListenerDump); err != nil {
		return err
	}
	diff := difflib.UnifiedDiff{
		FromFile: "Pilot Listeners",
		A:        difflib.SplitLines(pilotBytes.String()),
		ToFile:   "Envoy Listeners",
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
		fmt.Fprintln(c.w, "Listeners Match")
	}
	return nil
}
