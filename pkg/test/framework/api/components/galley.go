//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package components

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/pmezard/go-difflib/difflib"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/ids"
)

// Galley represents a deployed Galley instance.
type Galley interface {
	component.Instance

	// ApplyConfig applies the given config yaml file via Galley.
	ApplyConfig(yamlText string) error

	// ClearConfig clears all applied config so far.
	ClearConfig() error

	// SetMeshConfig applies the given mesh config yaml file via Galley.
	SetMeshConfig(yamlText string) error

	// WaitForSnapshot waits until the given snapshot is observed for the given type URL.
	WaitForSnapshot(collection string, validator SnapshotValidator) error
}

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

// SnapshotValidator interface for validating individual snapshot objects
type SnapshotValidator interface {
	ValidateObject(obj *SnapshotObject) error
}

// SnapshotValidatorFromFunc converts the given function into a SnapshotValidator.
func SnapshotValidatorFromFunc(fn func(obj *SnapshotObject) error) SnapshotValidator {
	return &fnValidator{
		fn: fn,
	}
}

var _ SnapshotValidator = &fnValidator{}

type fnValidator struct {
	fn func(obj *SnapshotObject) error
}

func (v *fnValidator) ValidateObject(obj *SnapshotObject) error {
	return v.fn(obj)
}

var _ SnapshotValidator = CompositeSnapshotValidator{}

// GoldenValidator creates a SnapshotValidator that tests for equivalence against
// the given golden object.
func GoldenValidator(golden map[string]interface{}) SnapshotValidator {
	return SnapshotValidatorFromFunc(func(actual *SnapshotObject) error {
		// Exclude ephemeral fields from comparison
		actual.Metadata.CreateTime = nil
		actual.Metadata.Version = ""

		// Convert the actual object to a map for comparison.
		b, err := json.Marshal(actual)
		if err != nil {
			return err
		}
		o := make(map[string]interface{})
		if err = json.Unmarshal(b, &o); err != nil {
			return err
		}

		if !reflect.DeepEqual(o, golden) {
			// They're not equal, generate an error.
			ajs, er := json.MarshalIndent(actual, "", "  ")
			if er != nil {
				return er
			}
			ejs, er := json.MarshalIndent(golden, "", "  ")
			if er != nil {
				return er
			}

			n := actual.Metadata.Name

			diff := difflib.UnifiedDiff{
				FromFile: fmt.Sprintf("Expected %q", n),
				A:        difflib.SplitLines(string(ejs)),
				ToFile:   fmt.Sprintf("Actual %q", n),
				B:        difflib.SplitLines(string(ajs)),
				Context:  100,
			}
			text, er := difflib.GetUnifiedDiffString(diff)
			if er != nil {
				return er
			}

			return fmt.Errorf("resource mismatch: %q\n%s", n, text)
		}

		// actual == expected
		return nil
	})
}

// CompositeSnapshotValidator is a map of validators indexed by object name.
type CompositeSnapshotValidator map[string]SnapshotValidator

// Validate is a SnapshotObjectValidator method that looks up the appropriate validate based on name.
func (v CompositeSnapshotValidator) ValidateObject(obj *SnapshotObject) error {
	if obj == nil {
		return errors.New("snapshot object is nil")
	}
	if obj.Metadata == nil {
		return errors.New("snapshot object metadata is nil")
	}

	validator := v[obj.Metadata.Name]
	if validator == nil {
		return fmt.Errorf("no validator found for object name %s", obj.Metadata.Name)
	}

	return validator.ValidateObject(obj)
}

// CompositeGoldenValidator creates an aggregate validator for the given map of golden objects indexed by name.
func CompositeGoldenValidator(goldenObjects []map[string]interface{}) (CompositeSnapshotValidator, error) {
	out := make(CompositeSnapshotValidator)
	for _, golden := range goldenObjects {
		name, err := extractName(golden)
		if err != nil {
			return nil, err
		}
		out[name] = GoldenValidator(golden)
	}
	return out, nil
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

// GetGalley from the repository
func GetGalley(e component.Repository, t testing.TB) Galley {
	t.Helper()
	return e.GetComponentOrFail(ids.Galley, t).(Galley)
}
