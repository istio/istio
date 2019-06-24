// Copyright 2017 Istio Authors
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

package values

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/kylelemons/godebug/diff"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
)

const (
	valuesFilesDir = "testdata/values"
)

func TestUnmarshalRealValues(t *testing.T) {
	files, err := getFilesInDir(valuesFilesDir)
	if err != nil {
		t.Fatalf("getFiles: %v", err)
	}

	for _, f := range files {
		fs, err := readFile(f)
		if err != nil {
			t.Fatalf("readFile: %v", err)
		}
		t.Logf("Testing file %s", f)
		v := &v1alpha2.Values{}
		err = yaml.Unmarshal([]byte(fs), v)
		if err != nil {
			t.Fatalf("yaml.Unmarshal(%s): got error %s", f, err)
		}
		s, err := yaml.Marshal(v)
		if err != nil {
			t.Fatalf("yaml.Marshal(%s): got error %s", f, err)
		}
		got, want := stripNL(string(s)), stripNL(fs)
		if !IsYAMLEqual(got, want) {
			t.Errorf("%s: got:\n%s\nwant:\n%s\n(-got, +want)\n%s\n", f, got, want, YAMLDiff(got, want))
		}

	}

}

func getFilesInDir(dirPath string) ([]string, error) {
	var files []string
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return files, nil
}

func readFile(path string) (string, error) {
	b, err := ioutil.ReadFile(path)
	return string(b), err
}

func stripNL(s string) string {
	return strings.Trim(s, "\n")
}

// TODO: move to util
func IsYAMLEqual(a, b string) bool {
	if strings.TrimSpace(a) == "" && strings.TrimSpace(b) == "" {
		return true
	}
	ajb, err := yaml.YAMLToJSON([]byte(a))
	if err != nil {
		return false
	}
	bjb, err := yaml.YAMLToJSON([]byte(b))
	if err != nil {
		return false
	}

	return string(ajb) == string(bjb)
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
