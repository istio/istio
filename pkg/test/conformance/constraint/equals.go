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

package constraint

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"github.com/pmezard/go-difflib/difflib"

	"istio.io/istio/pkg/test/util/tmpl"
)

// Equals checks for JSON based equality.
type Equals struct {
	Baseline interface{}
}

var _ json.Unmarshaler = &Equals{}
var _ Check = &Equals{}

// UnmarshalJSON implements json.Unmarshaler
func (e *Equals) UnmarshalJSON(b []byte) error {
	m := make(map[string]interface{})
	err := json.Unmarshal(b, &m)
	if err != nil {
		return err
	}

	var ok bool
	e.Baseline, ok = m["equals"]
	if !ok {
		return fmt.Errorf("unable to parse equals: %v", m)
	}
	return nil
}

// ValidateItem implements Check
func (e *Equals) ValidateItem(i interface{}, p Params) error {

	var iJSBytes []byte
	pro, ok := i.(proto.Message)
	if ok {
		// Use proto marshaller to ensure we get proper JSON representation first.
		m := jsonpb.Marshaler{
			EnumsAsInts: false,
			Indent:      "",
		}
		s, err := m.MarshalToString(pro)
		if err != nil {
			return err
		}

		// Run it through the standard JSON marshaller to get a well-formatted representation, suitable for comparison.
		mp := make(map[string]interface{})
		if err := json.Unmarshal([]byte(s), &mp); err != nil {
			return err
		}

		iJSBytes, err = json.MarshalIndent(mp, "", "  ")
		if err != nil {
			return err
		}
	} else {
		var err error
		iJSBytes, err = json.MarshalIndent(i, "", "  ")
		if err != nil {
			return err
		}
	}

	eJSBytes, err := json.MarshalIndent(e.Baseline, "", "  ")
	if err != nil {
		return err
	}

	var eJS string
	eJS, err = tmpl.Evaluate(string(eJSBytes), p)
	if err != nil {
		return err
	}

	if strings.TrimSpace(eJS) != strings.TrimSpace(string(iJSBytes)) {
		diff := difflib.UnifiedDiff{
			FromFile: "Expected",
			A:        difflib.SplitLines(eJS),
			ToFile:   "Actual",
			B:        difflib.SplitLines(string(iJSBytes)),
			Context:  100,
		}
		text, er := difflib.GetUnifiedDiffString(diff)
		if er != nil {
			return er
		}

		return fmt.Errorf("equals mismatch:\n%s", text)
	}

	return nil
}
