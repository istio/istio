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

package galley

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/gogo/protobuf/proto"
	"github.com/hashicorp/go-multierror"
	"github.com/pmezard/go-difflib/difflib"

	mcp "istio.io/api/mcp/v1alpha1"
)

// Ensure that Object can behave as a proto message.
var _ proto.Message = &SnapshotObject{}

// SnapshotObject contains a decoded versioned object with metadata received from the server.
type SnapshotObject struct {
	TypeURL  string        `protobuf:"bytes,1,opt,name=TypeURL,proto3" json:"TypeURL,omitempty"`
	Metadata *mcp.Metadata `protobuf:"bytes,2,opt,name=Metadata,proto3" json:"Metadata,omitempty"`
	Body     proto.Message `protobuf:"bytes,3,opt,name=Body,proto3" json:"Body,omitempty"`
}

func (m *SnapshotObject) Reset()         { *m = SnapshotObject{} }
func (m *SnapshotObject) String() string { return proto.CompactTextString(m) }
func (*SnapshotObject) ProtoMessage()    {}

// SnapshotValidatorFunc validates the given snapshot objects returned from Galley.
type SnapshotValidatorFunc func(actuals []*SnapshotObject) error

// NewSingleObjectSnapshotValidator creates a SnapshotValidatorFunc that ensures only a single object
// is found in the snapshot.
func NewSingleObjectSnapshotValidator(fn func(actual *SnapshotObject) error) SnapshotValidatorFunc {
	return func(actuals []*SnapshotObject) error {
		if len(actuals) != 1 {
			return fmt.Errorf("expected 1 resource, found %d", len(actuals))
		}
		return fn(actuals[0])
	}
}

// NewGoldenSnapshotValidator creates a SnapshotValidatorFunc that tests for equivalence against
// a set of golden object.
func NewGoldenSnapshotValidator(goldens []map[string]interface{}) SnapshotValidatorFunc {
	return func(actuals []*SnapshotObject) error {
		// Convert goldens to a map of JSON objects indexed by name.
		goldenMap := make(map[string]interface{})
		for _, g := range goldens {
			name, err := extractName(g)
			if err != nil {
				return err
			}
			goldenMap[name] = g
		}

		// Convert actuals to a map of JSON objects indexed by name
		actualMap := make(map[string]interface{})
		for _, a := range actuals {
			// Exclude ephemeral fields from comparison
			a := proto.Clone(a).(*SnapshotObject)
			a.Metadata.CreateTime = nil
			a.Metadata.Version = ""

			b, err := json.Marshal(a)
			if err != nil {
				return err
			}
			o := make(map[string]interface{})
			if err = json.Unmarshal(b, &o); err != nil {
				return err
			}

			name := a.Metadata.Name
			actualMap[name] = o
		}

		var err error

		for name, a := range actualMap {
			g, found := goldenMap[name]
			if !found {
				js, er := json.MarshalIndent(a, "", "  ")
				if er != nil {
					return er
				}
				err = multierror.Append(err, fmt.Errorf("unexpected resource found: %s\n%v", name, string(js)))
				continue
			}

			if !reflect.DeepEqual(a, g) {
				ajs, er := json.MarshalIndent(a, "", "  ")
				if er != nil {
					return er
				}
				gjs, er := json.MarshalIndent(g, "", "  ")
				if er != nil {
					return er
				}

				diff := difflib.UnifiedDiff{
					FromFile: fmt.Sprintf("Expected %q", name),
					A:        difflib.SplitLines(string(gjs)),
					ToFile:   fmt.Sprintf("Actual %q", name),
					B:        difflib.SplitLines(string(ajs)),
					Context:  100,
				}
				text, er := difflib.GetUnifiedDiffString(diff)
				if er != nil {
					return er
				}

				err = multierror.Append(err, fmt.Errorf("resource mismatch: %q\n%s",
					name, text))
			}
		}

		for name, golden := range goldenMap {
			_, found := actualMap[name]
			if !found {
				js, er := json.MarshalIndent(golden, "", "  ")
				if er != nil {
					return er
				}
				err = multierror.Append(err, fmt.Errorf("expected resource not found: %s\n%v", name, js))
				continue
			}
		}

		return err
	}
}

func extractName(i map[string]interface{}) (string, error) {
	m, found := i["Metadata"]
	if !found {
		return "", fmt.Errorf("metadata section not found in resource")
	}

	meta, ok := m.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("metadata section is not a map")
	}

	n, found := meta["name"]
	if !found {
		return "", fmt.Errorf("metadata section does not contain name")
	}

	name, ok := n.(string)
	if !ok {
		return "", fmt.Errorf("name field is not a string")
	}

	return name, nil
}
