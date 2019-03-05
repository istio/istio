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
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/d4l3k/messagediff.v1"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"k8s.io/client-go/util/jsonpath"
)

var fixupNumericJSONComparison = regexp.MustCompile(`([=<>]+)\s*([0-9]+)\s*\)`)
var fixupAttributeReference = regexp.MustCompile(`\[\s*'[^']+\s*'\s*]`)

type Structpath struct {
	structure interface{}
	isJSON    bool
	t         *testing.T
}

// Creates a new Structpath by marshaling the proto to JSON and then evaluating over that
// structure. This is the most generally useful form as serialization to JSON also automatically
// converts proto.Any and proto.Struct to the serialized JSON forms which can then be evaluated
// over. The downside is the loss of type fidelity for numeric types as JSON can only represent
// floats.
func AssertThatProto(t *testing.T, proto proto.Message) *Structpath {
	if proto == nil {
		t.Fatal("expected non-nil proto")
	}
	parsed := protoToParsedJSON(proto, t)
	return &Structpath{parsed, true, t}
}

func protoToParsedJSON(message proto.Message, t *testing.T) interface{} {
	// Convert proto to json and then parse into struct
	jsonText, err := (&jsonpb.Marshaler{Indent: " "}).MarshalToString(message)
	if err != nil {
		t.Fatalf("failed to convert proto to JSON: %v", err)
	}
	var parsed interface{}
	err = json.Unmarshal([]byte(jsonText), &parsed)
	if err != nil {
		t.Fatalf("failed to parse into JSON struct: %v", err)
	}
	return parsed
}

func (p *Structpath) ForTest(t *testing.T) *Structpath {
	return &Structpath{p.structure, p.isJSON, t}
}

func (p *Structpath) Accept(path string, args ...interface{}) bool {
	p.t.Helper()
	path = fmt.Sprintf(path, args...)
	parser := jsonpath.New("path")
	err := parser.Parse(p.fixPath(path))
	if err != nil {
		p.t.Fatalf("invalid path: %v - %v", path, err)
	}
	values, err := parser.AllowMissingKeys(true).FindResults(p.structure)
	if err != nil {
		p.t.Fatalf("err finding results for path: %v - %v", path, err)
	}
	if len(values[0]) > 0 {
		return true
	}
	return false
}

func (p *Structpath) Select(path string, args ...interface{}) *Structpath {
	p.t.Helper()
	path = fmt.Sprintf(path, args...)
	value := p.findValue(path)
	if value == nil {
		p.t.Fatalf("Cannot select non-existent path: %v", path)
	}
	return &Structpath{value, p.isJSON, p.t}
}

func (p *Structpath) Equals(expected interface{}, path string, args ...interface{}) *Structpath {
	p.t.Helper()
	typeOf := reflect.TypeOf(expected)
	protoMessageType := reflect.TypeOf((*proto.Message)(nil)).Elem()
	if typeOf.Implements(protoMessageType) {
		return p.equalsStruct(expected.(proto.Message), path, args...)
	}
	switch kind := typeOf.Kind(); kind {
	case reflect.String:
		return p.equalsString(reflect.ValueOf(expected).String(), path, args...)
	case reflect.Bool:
		return p.equalsBool(reflect.ValueOf(expected).Bool(), path, args...)
	case reflect.Float32, reflect.Float64:
		return p.equalsNumber(reflect.ValueOf(expected).Float(), path, args...)
	case reflect.Int, reflect.Int8, reflect.Int32, reflect.Int64:
		return p.equalsNumber(float64(reflect.ValueOf(expected).Int()), path, args...)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return p.equalsNumber(float64(reflect.ValueOf(expected).Uint()), path, args...)
	case protoMessageType.Kind():
	}
	// TODO: Add struct support
	p.t.Fatalf("Attempt to call Equals for unsupported type: %v", expected)
	return nil
}

func (p *Structpath) equalsString(expected string, path string, args ...interface{}) *Structpath {
	p.t.Helper()
	path = fmt.Sprintf(path, args...)
	value := p.execute(path)
	if value != expected {
		p.t.Fatalf("Expected %v but got %v for path %v", expected, value, path)
	}
	return p
}

func (p *Structpath) equalsNumber(expected float64, path string, args ...interface{}) *Structpath {
	p.t.Helper()
	path = fmt.Sprintf(path, args...)
	result := reflect.ValueOf(p.findValue(path)).Float()
	if result != expected {
		p.t.Fatalf("Expected %v but got %v for path %v", expected, result, path)
	}
	return p
}

func (p *Structpath) equalsBool(expected bool, path string, args ...interface{}) *Structpath {
	p.t.Helper()
	path = fmt.Sprintf(path, args...)
	result := reflect.ValueOf(p.findValue(path)).Bool()
	if result != expected {
		p.t.Fatalf("Expected %v but got %v for path %v", expected, result, path)
	}
	return p
}

func (p *Structpath) equalsStruct(proto proto.Message, path string, args ...interface{}) *Structpath {
	p.t.Helper()
	path = fmt.Sprintf(path, args...)
	jsonStruct := protoToParsedJSON(proto, p.t)
	diff, b := messagediff.PrettyDiff(reflect.ValueOf(p.findValue(path)).Interface(), jsonStruct)
	if !b {
		p.t.Fatalf("structs did not match: %v", diff)
	}
	return p
}

func (p *Structpath) Exists(path string, args ...interface{}) *Structpath {
	p.t.Helper()
	path = fmt.Sprintf(path, args...)
	if p.findValue(path) == nil {
		p.t.Fatalf("No entry exists at path: %v", path)
	}
	return p
}

func (p *Structpath) NotExists(path string, args ...interface{}) *Structpath {
	p.t.Helper()
	path = fmt.Sprintf(path, args...)
	parser := jsonpath.New("path")
	err := parser.Parse(p.fixPath(path))
	if err != nil {
		p.t.Fatalf("invalid path: %v - %v", path, err)
	}
	values, err := parser.AllowMissingKeys(true).FindResults(p.structure)
	if err != nil {
		p.t.Fatalf("err finding results for path: %v - %v", path, err)
	}
	if len(values[0]) > 0 {
		p.t.Fatalf("Expected no result but got: %v for path: %v", values[0], path)
	}
	return p
}

func (p *Structpath) execute(path string) string {
	p.t.Helper()
	parser := jsonpath.New("path")
	err := parser.Parse(p.fixPath(path))
	if err != nil {
		p.t.Fatalf("invalid path: %v - %v", path, err)
	}
	buf := new(bytes.Buffer)
	err = parser.Execute(buf, p.structure)
	if err != nil {
		p.t.Fatalf("err finding results for path: %v - %v", path, err)
	}
	return buf.String()
}

func (p *Structpath) findValue(path string) interface{} {
	p.t.Helper()
	parser := jsonpath.New("path")
	err := parser.Parse(p.fixPath(path))
	if err != nil {
		p.t.Fatalf("invalid path: %v - %v", path, err)
	}
	values, err := parser.FindResults(p.structure)
	if err != nil {
		p.t.Fatalf("err finding results for path: %v - %v", path, err)
	}
	if len(values) == 0 {
		p.t.Fatalf("no value for path: %v", path)
	}
	return values[0][0].Interface()
}

// Fixes up some quirks in jsonpath handling.
// See https://github.com/kubernetes/client-go/issues/553
func (p *Structpath) fixPath(path string) string {
	// jsonpath doesn't handle numeric comparisons in a tolerant way. All json numbers are floats
	// and filter expressions on the form {.x[?(@.some.value==123]} won't work but
	// {.x[?(@.some.value==123.0]} will.
	result := path
	if p.isJSON {
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
