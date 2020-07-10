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

	"github.com/golang/protobuf/jsonpb"
	"github.com/pmezard/go-difflib/difflib"
)

// ListenerDiff prints a diff between Istiod and Envoy listeners to the passed writer
func (c *Comparator) ListenerDiff() error {
	jsonm := &jsonpb.Marshaler{Indent: "   "}
	envoyBytes, istiodBytes := &bytes.Buffer{}, &bytes.Buffer{}
	envoyListenerDump, err := c.envoy.GetDynamicListenerDump(true)
	if err != nil {
		envoyBytes.WriteString(err.Error())
	} else if err := jsonm.Marshal(envoyBytes, envoyListenerDump); err != nil {
		return err
	}
	istiodListenerDump, err := c.istiod.GetDynamicListenerDump(true)
	if err != nil {
		istiodBytes.WriteString(err.Error())
	} else if err := jsonm.Marshal(istiodBytes, istiodListenerDump); err != nil {
		return err
	}
	diff := difflib.UnifiedDiff{
		FromFile: "Istiod Listeners",
		A:        difflib.SplitLines(istiodBytes.String()),
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
