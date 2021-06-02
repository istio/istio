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

package util

import (
	"bytes"
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	yaml2 "github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"github.com/kylelemons/godebug/diff"
	"sigs.k8s.io/yaml"
)

// ToYAML returns a YAML string representation of val, or the error string if an error occurs.
func ToYAML(val interface{}) string {
	y, err := yaml2.Marshal(val)
	if err != nil {
		return err.Error()
	}
	return string(y)
}

// ToYAMLWithJSONPB returns a YAML string representation of val (using jsonpb), or the error string if an error occurs.
func ToYAMLWithJSONPB(val proto.Message) string {
	m := jsonpb.Marshaler{EnumsAsInts: true}
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
	m := jsonpb.Marshaler{EnumsAsInts: true}
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
func UnmarshalWithJSONPB(y string, out proto.Message, allowUnknownField bool) error {
	// Treat nothing as nothing.  If we called jsonpb.Unmarshaler it would return the same.
	if y == "" {
		return nil
	}
	jb, err := yaml.YAMLToJSON([]byte(y))
	if err != nil {
		return err
	}
	u := jsonpb.Unmarshaler{AllowUnknownFields: allowUnknownField}
	err = u.Unmarshal(bytes.NewReader(jb), out)
	if err != nil {
		return err
	}
	return nil
}

// OverlayTrees performs a sequential JSON strategic of overlays over base.
func OverlayTrees(base map[string]interface{}, overlays ...map[string]interface{}) (map[string]interface{}, error) {
	bby, err := yaml.Marshal(base)
	if err != nil {
		return nil, err
	}
	by := string(bby)

	for _, o := range overlays {
		oy, err := yaml.Marshal(o)
		if err != nil {
			return nil, err
		}

		by, err = OverlayYAML(by, string(oy))
		if err != nil {
			return nil, err
		}
	}

	out := make(map[string]interface{})
	err = yaml.Unmarshal([]byte(by), &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// OverlayYAML patches the overlay tree over the base tree and returns the result. All trees are expressed as YAML
// strings.
func OverlayYAML(base, overlay string) (string, error) {
	if strings.TrimSpace(base) == "" {
		return overlay, nil
	}
	if strings.TrimSpace(overlay) == "" {
		return base, nil
	}
	bj, err := yaml2.YAMLToJSON([]byte(base))
	if err != nil {
		return "", fmt.Errorf("yamlToJSON error in base: %s\n%s", err, bj)
	}
	oj, err := yaml2.YAMLToJSON([]byte(overlay))
	if err != nil {
		return "", fmt.Errorf("yamlToJSON error in overlay: %s\n%s", err, oj)
	}
	if base == "" {
		bj = []byte("{}")
	}
	if overlay == "" {
		oj = []byte("{}")
	}

	merged, err := jsonpatch.MergePatch(bj, oj)
	if err != nil {
		return "", fmt.Errorf("json merge error (%s) for base object: \n%s\n override object: \n%s", err, bj, oj)
	}
	my, err := yaml2.JSONToYAML(merged)
	if err != nil {
		return "", fmt.Errorf("jsonToYAML error (%s) for merged object: \n%s", err, merged)
	}

	return string(my), nil
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

// YAMLReducedDiff this method compares two yaml strings to determine
// the differences between the two. It uses github.com/kylelemons/godebug/diff
// library as a base. This method will keep lines based on `contextNoOfLines`
// parameter to provide context of the changes. Added lines start with plus
// sign and will appear to be `Green`, removed lines start with minus sign,
// will appear to be `Red`. ... represent same content from both strings.
func YAMLReducedDiff(a, b string, contextNoOfLines int) string {
	aLines := strings.Split(a, "\n")
	bLines := strings.Split(b, "\n")

	chunks := diff.DiffChunks(aLines, bLines)
	buf := new(bytes.Buffer)
	isChunkImportant := func(chunk diff.Chunk) bool {
		// When only one element exists
		if len(chunk.Added)+len(chunk.Deleted)+len(chunk.Equal) == 1 {
			all := append(chunk.Added, chunk.Deleted...)
			all = append(all, chunk.Equal...)
			return len(strings.Join(all, "")) > 0
		}
		return true
	}
	if len(chunks) > 0 && !isChunkImportant(chunks[0]) {
		chunks = chunks[1:]
	}
	if len(chunks) > 0 && !isChunkImportant(chunks[len(chunks)-1]) {
		chunks = chunks[0 : len(chunks)-1]
	}

	noOfChunks := len(chunks)
	for i, c := range chunks {
		for _, line := range c.Added {
			fmt.Fprintf(buf, "\033[32m+%s\033[0m\n", line)
		}
		for _, line := range c.Deleted {
			fmt.Fprintf(buf, "\033[31m-%s\033[0m\n", line)
		}

		equalNoOfLines := len(c.Equal)
		var linesToKeep []string
		switch {
		case equalNoOfLines <= contextNoOfLines:
			linesToKeep = c.Equal
		case equalNoOfLines <= contextNoOfLines*2:
			if i < noOfChunks-1 {
				// this is not the last chunk
				if len(c.Added) > 0 || len(c.Deleted) > 0 {
					linesToKeep = c.Equal
				} else {
					linesToKeep = c.Equal[equalNoOfLines-contextNoOfLines:]
				}
			} else {
				// this is the last chunk
				linesToKeep = c.Equal[0:contextNoOfLines]
			}
		default:
			if i < noOfChunks-1 {
				// this is not the last chunk
				if len(c.Added) > 0 || len(c.Deleted) > 0 {
					linesToKeep = c.Equal[0:contextNoOfLines]
					linesToKeep = append(linesToKeep, "...")
				}
				linesToKeep = append(linesToKeep, c.Equal[equalNoOfLines-contextNoOfLines:]...)
			} else {
				// this is the last chunk
				linesToKeep = c.Equal[0:contextNoOfLines]
			}
		}

		for _, line := range linesToKeep {
			fmt.Fprintf(buf, " %s\n", line)
		}
	}
	return strings.TrimRight(buf.String(), "\n")
}

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

// IsYAMLEmpty reports whether the YAML string y is logically empty.
func IsYAMLEmpty(y string) bool {
	var yc []string
	for _, l := range strings.Split(y, "\n") {
		yt := strings.TrimSpace(l)
		if !strings.HasPrefix(yt, "#") && !strings.HasPrefix(yt, "---") {
			yc = append(yc, l)
		}
	}
	res := strings.TrimSpace(strings.Join(yc, "\n"))
	return res == "{}" || res == ""
}
