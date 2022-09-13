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

package structpath

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"k8s.io/client-go/util/jsonpath"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/protomarshal"
)

var (
	fixupNumericJSONComparison = regexp.MustCompile(`([=<>]+)\s*([0-9]+)\s*\)`)
	fixupAttributeReference    = regexp.MustCompile(`\[\s*'[^']+\s*'\s*]`)
)

type Instance struct {
	structure     any
	isJSON        bool
	constraints   []constraint
	creationError error
}

type constraint func() error

// ForProto creates a structpath Instance by marshaling the proto to JSON and then evaluating over that
// structure. This is the most generally useful form as serialization to JSON also automatically
// converts proto.Any and proto.Struct to the serialized JSON forms which can then be evaluated
// over. The downside is the loss of type fidelity for numeric types as JSON can only represent
// floats.
func ForProto(proto proto.Message) *Instance {
	if proto == nil {
		return newErrorInstance(errors.New("expected non-nil proto"))
	}

	parsed, err := protoToParsedJSON(proto)
	if err != nil {
		return newErrorInstance(err)
	}

	i := &Instance{
		isJSON:    true,
		structure: parsed,
	}
	i.structure = parsed
	return i
}

func newErrorInstance(err error) *Instance {
	return &Instance{
		isJSON:        true,
		creationError: err,
	}
}

func protoToParsedJSON(message proto.Message) (any, error) {
	// Convert proto to json and then parse into struct
	jsonText, err := protomarshal.MarshalIndent(message, "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to convert proto to JSON: %v", err)
	}
	var parsed any
	err = json.Unmarshal(jsonText, &parsed)
	if err != nil {
		return nil, fmt.Errorf("failed to parse into JSON struct: %v", err)
	}
	return parsed, nil
}

func (i *Instance) Select(path string, args ...any) *Instance {
	if i.creationError != nil {
		// There was an error during the creation of this Instance. Just return the
		// same instance since it will error on Check anyway.
		return i
	}

	path = fmt.Sprintf(path, args...)
	value, err := i.findValue(path)
	if err != nil {
		return newErrorInstance(err)
	}
	if value == nil {
		return newErrorInstance(fmt.Errorf("cannot select non-existent path: %v", path))
	}

	// Success.
	return &Instance{
		isJSON:    i.isJSON,
		structure: value,
	}
}

func (i *Instance) appendConstraint(fn func() error) *Instance {
	i.constraints = append(i.constraints, fn)
	return i
}

func (i *Instance) Equals(expected any, path string, args ...any) *Instance {
	path = fmt.Sprintf(path, args...)
	return i.appendConstraint(func() error {
		typeOf := reflect.TypeOf(expected)
		protoMessageType := reflect.TypeOf((*proto.Message)(nil)).Elem()
		if typeOf.Implements(protoMessageType) {
			return i.equalsStruct(expected.(proto.Message), path)
		}
		switch kind := typeOf.Kind(); kind {
		case reflect.String:
			return i.equalsString(reflect.ValueOf(expected).String(), path)
		case reflect.Bool:
			return i.equalsBool(reflect.ValueOf(expected).Bool(), path)
		case reflect.Float32, reflect.Float64:
			return i.equalsNumber(reflect.ValueOf(expected).Float(), path)
		case reflect.Int, reflect.Int8, reflect.Int32, reflect.Int64:
			return i.equalsNumber(float64(reflect.ValueOf(expected).Int()), path)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return i.equalsNumber(float64(reflect.ValueOf(expected).Uint()), path)
		case protoMessageType.Kind():
		}
		// TODO: Add struct support
		return fmt.Errorf("attempt to call Equals for unsupported type: %v", expected)
	})
}

func (i *Instance) ContainSubstring(substr, path string) *Instance {
	return i.appendConstraint(func() error {
		value, err := i.execute(path)
		if err != nil {
			return err
		}
		if found := strings.Contains(value, substr); !found {
			return fmt.Errorf("substring %v did not match: %v", substr, value)
		}
		return nil
	})
}

func (i *Instance) equalsString(expected string, path string) error {
	value, err := i.execute(path)
	if err != nil {
		return err
	}
	if value != expected {
		return fmt.Errorf("expected %v but got %v for path %v", expected, value, path)
	}
	return nil
}

func (i *Instance) equalsNumber(expected float64, path string) error {
	v, err := i.findValue(path)
	if err != nil {
		return err
	}
	result := reflect.ValueOf(v).Float()
	if result != expected {
		return fmt.Errorf("expected %v but got %v for path %v", expected, result, path)
	}
	return nil
}

func (i *Instance) equalsBool(expected bool, path string) error {
	v, err := i.findValue(path)
	if err != nil {
		return err
	}
	result := reflect.ValueOf(v).Bool()
	if result != expected {
		return fmt.Errorf("expected %v but got %v for path %v", expected, result, path)
	}
	return nil
}

func (i *Instance) equalsStruct(proto proto.Message, path string) error {
	jsonStruct, err := protoToParsedJSON(proto)
	if err != nil {
		return err
	}
	v, err := i.findValue(path)
	if err != nil {
		return err
	}
	diff := cmp.Diff(reflect.ValueOf(v).Interface(), jsonStruct)
	if diff != "" {
		return fmt.Errorf("structs did not match: %v", diff)
	}
	return nil
}

func (i *Instance) Exists(path string, args ...any) *Instance {
	path = fmt.Sprintf(path, args...)
	return i.appendConstraint(func() error {
		v, err := i.findValue(path)
		if err != nil {
			return err
		}
		if v == nil {
			return fmt.Errorf("no entry exists at path: %v", path)
		}
		return nil
	})
}

func (i *Instance) NotExists(path string, args ...any) *Instance {
	path = fmt.Sprintf(path, args...)
	return i.appendConstraint(func() error {
		parser := jsonpath.New("path")
		err := parser.Parse(i.fixPath(path))
		if err != nil {
			return fmt.Errorf("invalid path: %v - %v", path, err)
		}
		values, err := parser.AllowMissingKeys(true).FindResults(i.structure)
		if err != nil {
			return fmt.Errorf("err finding results for path: %v - %v", path, err)
		}
		if len(values) == 0 {
			return nil
		}
		if len(values[0]) > 0 {
			return fmt.Errorf("expected no result but got: %v for path: %v", values[0], path)
		}
		return nil
	})
}

// Check executes the set of constraints for this selection
// and returns the first error encountered, or nil if all constraints
// have been successfully met. All constraints are removed after them
// check is performed.
func (i *Instance) Check() error {
	// After the check completes, clear out the constraints.
	defer func() {
		i.constraints = i.constraints[:0]
	}()

	// If there was a creation error, just return that immediately.
	if i.creationError != nil {
		return i.creationError
	}

	for _, c := range i.constraints {
		if err := c(); err != nil {
			return err
		}
	}
	return nil
}

// CheckOrFail calls Check on this selection and fails the given test if an
// error is encountered.
func (i *Instance) CheckOrFail(t test.Failer) *Instance {
	t.Helper()
	if err := i.Check(); err != nil {
		t.Fatal(err)
	}
	return i
}

func (i *Instance) execute(path string) (string, error) {
	parser := jsonpath.New("path")
	err := parser.Parse(i.fixPath(path))
	if err != nil {
		return "", fmt.Errorf("invalid path: %v - %v", path, err)
	}
	buf := new(bytes.Buffer)
	err = parser.Execute(buf, i.structure)
	if err != nil {
		return "", fmt.Errorf("err finding results for path: %v - %v", path, err)
	}
	return buf.String(), nil
}

func (i *Instance) findValue(path string) (any, error) {
	parser := jsonpath.New("path")
	err := parser.Parse(i.fixPath(path))
	if err != nil {
		return nil, fmt.Errorf("invalid path: %v - %v", path, err)
	}
	values, err := parser.FindResults(i.structure)
	if err != nil {
		return nil, fmt.Errorf("err finding results for path: %v: %v. Structure: %v", path, err, i.structure)
	}
	if len(values) == 0 || len(values[0]) == 0 {
		return nil, fmt.Errorf("no value for path: %v", path)
	}
	return values[0][0].Interface(), nil
}

// Fixes up some quirks in jsonpath handling.
// See https://github.com/kubernetes/client-go/issues/553
func (i *Instance) fixPath(path string) string {
	// jsonpath doesn't handle numeric comparisons in a tolerant way. All json numbers are floats
	// and filter expressions on the form {.x[?(@.some.value==123]} won't work but
	// {.x[?(@.some.value==123.0]} will.
	result := path
	if i.isJSON {
		template := "$1$2.0)"
		result = fixupNumericJSONComparison.ReplaceAllString(path, template)
	}
	// jsonpath doesn't like map literal references that contain periods. I.e
	// you can't do x['user.map'] but x.user\.map works so we just translate to that
	result = string(fixupAttributeReference.ReplaceAllFunc([]byte(result), func(i []byte) []byte {
		input := string(i)
		input = strings.Replace(input, "[", "", 1)
		input = strings.Replace(input, "]", "", 1)
		input = strings.Replace(input, "'", "", 2)
		parts := strings.Split(input, ".")
		output := "."
		for i := 0; i < len(parts)-1; i++ {
			output += parts[i]
			output += "\\."
		}
		output += parts[len(parts)-1]
		return []byte(output)
	}))

	return result
}
