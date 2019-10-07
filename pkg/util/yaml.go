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

package util

import (
	"bytes"
	"strings"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	jsonpb2 "github.com/golang/protobuf/jsonpb"
	"github.com/kylelemons/godebug/diff"
	"sigs.k8s.io/yaml"
)

// IsYAMLEqual reports whether the YAML in strings a and b are equal.
func IsYAMLEqual(a, b string) bool {
	if strings.TrimSpace(a) == "" && strings.TrimSpace(b) == "" {
		return true
	}
	ajb, err := yaml.YAMLToJSON([]byte(a))
	if err != nil {
		scope.Debugf("bad YAML in isYAMLEqual:\n%s", a)
		return false
	}
	bjb, err := yaml.YAMLToJSON([]byte(b))
	if err != nil {
		scope.Debugf("bad YAML in isYAMLEqual:\n%s", b)
		return false
	}

	return string(ajb) == string(bjb)
}

// ToYAML returns a YAML string representation of val, or the error string if an error occurs.
func ToYAML(val interface{}) string {
	y, err := yaml.Marshal(val)
	if err != nil {
		return err.Error()
	}
	return string(y)
}

// ToYAMLWithJSONPB returns a YAML string representation of val (using jsonpb), or the error string if an error occurs.
func ToYAMLWithJSONPB(val proto.Message) string {
	m := jsonpb.Marshaler{}
	js, err := m.MarshalToString(val)
	if err != nil {
		return err.Error()
	}
	yb, err := yaml.JSONToYAML([]byte(js))
	if err != nil {
		return err.Error()
	}
	return string(yb)
}

// MarshalWithJSONPB returns a YAML string representation of val (using jsonpb).
func MarshalWithJSONPB(val proto.Message) (string, error) {
	m := jsonpb.Marshaler{}
	js, err := m.MarshalToString(val)
	if err != nil {
		return "", err
	}
	yb, err := yaml.JSONToYAML([]byte(js))
	if err != nil {
		return "", err
	}
	return string(yb), nil
}

// UnmarshalWithJSONPB unmarshals y into out using gogo jsonpb (required for many proto defined structs).
func UnmarshalWithJSONPB(y string, out proto.Message) error {
	jb, err := yaml.YAMLToJSON([]byte(y))
	if err != nil {
		return err
	}

	u := jsonpb.Unmarshaler{AllowUnknownFields: false}
	err = u.Unmarshal(bytes.NewReader(jb), out)
	if err != nil {
		return err
	}
	return nil
}

// UnmarshalValuesWithJSONPB unmarshals y into out using golang jsonpb.
func UnmarshalValuesWithJSONPB(y string, out proto.Message) error {
	jb, err := yaml.YAMLToJSON([]byte(y))
	if err != nil {
		return err
	}
	u := jsonpb2.Unmarshaler{AllowUnknownFields: true}
	err = u.Unmarshal(bytes.NewReader(jb), out)
	if err != nil {
		return err
	}
	return nil
}

func YAMLDiff(a, b string) string {
	ao, bo := make(map[string]interface{}), make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(a), &ao); err != nil {
		return err.Error()
	}
	if err := yaml.Unmarshal([]byte(b), &bo); err != nil {
		return err.Error()
	}

	ay, err := yaml.Marshal(ao)
	if err != nil {
		return err.Error()
	}
	by, err := yaml.Marshal(bo)
	if err != nil {
		return err.Error()
	}

	return diff.Diff(string(ay), string(by))
}

/*func ObjectsInManifest(mstr string) string {
	ao, err := manifest.ParseObjectsFromYAMLManifest(mstr)
	if err != nil {
		return err.Error()
	}
	var out []string
	for _, v := range ao {
		out = append(out, v.Hash())
	}
	return strings.Join(out, "\n")
}*/
