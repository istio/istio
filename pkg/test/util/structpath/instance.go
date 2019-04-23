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

package structpath

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"

	messagediff "gopkg.in/d4l3k/messagediff.v1"

	"k8s.io/client-go/util/jsonpath"
)

var (
	fixupNumericJSONComparison = regexp.MustCompile(`([=<>]+)\s*([0-9]+)\s*\)`)
	fixupAttributeReference    = regexp.MustCompile(`\[\s*'[^']+\s*'\s*]`)
)

type constraint func() error

type Instance struct {
	structure   interface{}
	isJSON      bool
	constraints []constraint
}

// ForProto creates a structpath Instance by marshaling the proto to JSON and then evaluating over that
// structure. This is the most generally useful form as serialization to JSON also automatically
// converts proto.Any and proto.Struct to the serialized JSON forms which can then be evaluated
// over. The downside is the loss of type fidelity for numeric types as JSON can only represent
// floats.
func ForProto(proto proto.Message) *Instance {
	i := &Instance{isJSON: true}

	if proto == nil {
		return i.appendError(errors.New("expected non-nil proto"))
	}

	parsed, err := protoToParsedJSON(proto)
	if err != nil {
		return i.appendError(err)
	}

	i.structure = parsed
	return i
}

func protoToParsedJSON(message proto.Message) (interface{}, error) {
	// Convert proto to json and then parse into struct
	jsonText, err := (&jsonpb.Marshaler{Indent: " "}).MarshalToString(message)
	if err != nil {
		return nil, fmt.Errorf("failed to convert proto to JSON: %v", err)
	}
	var parsed interface{}
	err = json.Unmarshal([]byte(jsonText), &parsed)
	if err != nil {
		return nil, fmt.Errorf("failed to parse into JSON struct: %v", err)
	}
	return parsed, nil
}

func (i *Instance) Select(path string, args ...interface{}) *Instance {
	path = fmt.Sprintf(path, args...)
	value, err := i.findValue(path)
	if err != nil {
		return i.appendError(err)
	}
	if value == nil {
		return i.appendError(fmt.Errorf("cannot select non-existent path: %v", path))
	}
	return &Instance{
		structure: value,
		isJSON:    i.isJSON,
	}
}

func (i *Instance) appendConstraint(c constraint) *Instance {
	i.constraints = append(i.constraints, c)
	return i
}

func (i *Instance) appendError(err error) *Instance {
	return i.appendConstraint(func() error {
		return err
	})
}

func (i *Instance) Equals(expected interface{}, path string, args ...interface{}) *Instance {
	i.appendConstraint(func() error {
		typeOf := reflect.TypeOf(expected)
		protoMessageType := reflect.TypeOf((*proto.Message)(nil)).Elem()
		if typeOf.Implements(protoMessageType) {
			return i.equalsStruct(expected.(proto.Message), path, args...)
		}
		switch kind := typeOf.Kind(); kind {
		case reflect.String:
			return i.equalsString(reflect.ValueOf(expected).String(), path, args...)
		case reflect.Bool:
			return i.equalsBool(reflect.ValueOf(expected).Bool(), path, args...)
		case reflect.Float32, reflect.Float64:
			return i.equalsNumber(reflect.ValueOf(expected).Float(), path, args...)
		case reflect.Int, reflect.Int8, reflect.Int32, reflect.Int64:
			return i.equalsNumber(float64(reflect.ValueOf(expected).Int()), path, args...)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return i.equalsNumber(float64(reflect.ValueOf(expected).Uint()), path, args...)
		case protoMessageType.Kind():
		}
		// TODO: Add struct support
		return fmt.Errorf("attempt to call Equals for unsupported type: %v", expected)
	})

	return i
}

func (i *Instance) equalsString(expected string, path string, args ...interface{}) error {
	path = fmt.Sprintf(path, args...)
	value, err := i.execute(path)
	if err != nil {
		return err
	}
	if value != expected {
		return fmt.Errorf("expected %v but got %v for path %v", expected, value, path)
	}
	return nil
}

func (i *Instance) equalsNumber(expected float64, path string, args ...interface{}) error {
	path = fmt.Sprintf(path, args...)
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

func (i *Instance) equalsBool(expected bool, path string, args ...interface{}) error {
	path = fmt.Sprintf(path, args...)
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

func (i *Instance) equalsStruct(proto proto.Message, path string, args ...interface{}) error {
	path = fmt.Sprintf(path, args...)
	jsonStruct, err := protoToParsedJSON(proto)
	if err != nil {
		return err
	}
	v, err := i.findValue(path)
	if err != nil {
		return err
	}
	diff, b := messagediff.PrettyDiff(reflect.ValueOf(v).Interface(), jsonStruct)
	if !b {
		return fmt.Errorf("structs did not match: %v", diff)
	}
	return nil
}

func (i *Instance) Exists(path string, args ...interface{}) *Instance {
	i.appendConstraint(func() error {
		path = fmt.Sprintf(path, args...)
		v, err := i.findValue(path)
		if err != nil {
			return err
		}
		if v == nil {
			return fmt.Errorf("no entry exists at path: %v", path)
		}
		return nil
	})

	return i
}

func (i *Instance) NotExists(path string, args ...interface{}) *Instance {
	i.appendConstraint(func() error {
		path = fmt.Sprintf(path, args...)
		parser := jsonpath.New("path")
		err := parser.Parse(i.fixPath(path))
		if err != nil {
			return fmt.Errorf("invalid path: %v - %v", path, err)
		}
		values, err := parser.AllowMissingKeys(true).FindResults(i.structure)
		if err != nil {
			return fmt.Errorf("err finding results for path: %v - %v", path, err)
		}
		if len(values[0]) > 0 {
			return fmt.Errorf("expected no result but got: %v for path: %v", values[0], path)
		}
		return nil
	})
	return i
}

// Check executes the set of contstraints for this selection
// and returns the first error encountered, or nil if all constraints
// have been successfully met.
func (i *Instance) Check() error {
	for _, c := range i.constraints {
		if err := c(); err != nil {
			return err
		}
	}
	return nil
}

// CheckOrFail calls Check on this selection and fails the given test if an
// error is encountered.
func (i *Instance) CheckOrFail(t *testing.T) *Instance {
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

func (i *Instance) findValue(path string) (interface{}, error) {
	parser := jsonpath.New("path")
	err := parser.Parse(i.fixPath(path))
	if err != nil {
		return nil, fmt.Errorf("invalid path: %v - %v", path, err)
	}
	values, err := parser.FindResults(i.structure)
	if err != nil {
		return nil, fmt.Errorf("err finding results for path: %v - %v", path, err)
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
