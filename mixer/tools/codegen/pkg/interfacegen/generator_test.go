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

package interfacegen

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"testing"
)

type logFn func(string, ...interface{})

// TestGenerator_Generate uses the outputs file descriptors generated via bazel
// and compares them against the golden files.
func TestGenerator_Generate(t *testing.T) {
	importmap := map[string]string{
		"mixer/v1/config/descriptor/value_type.proto":   "istio.io/api/mixer/v1/config/descriptor",
		"pkg/adapter/template/TemplateExtensions.proto": "istio.io/mixer/pkg/adapter/template",
		"gogoproto/gogo.proto":                          "github.com/gogo/protobuf/gogoproto",
		"google/protobuf/duration.proto":                "github.com/gogo/protobuf/types",
	}

	tests := []struct {
		name, descriptor, wantIntFace, wantProto string
	}{
		{"Metrics", "testdata/metric_template_library_proto.descriptor_set",
			"testdata/MetricTemplateProcessorInterface.golden.go",
			"testdata/MetricTemplateGenerated.golden.proto"},
		{"Quota", "testdata/quota_template_library_proto.descriptor_set",
			"testdata/QuotaTemplateProcessorInterface.golden.go",
			"testdata/QuotaTemplateGenerated.golden.proto"},
		{"Logs", "testdata/log_template_library_proto.descriptor_set",
			"testdata/LogTemplateProcessorInterface.golden.go",
			"testdata/LogTemplateGenerated.golden.proto"},
		{"Lists", "testdata/list_template_library_proto.descriptor_set",
			"testdata/ListTemplateProcessorInterface.golden.go",
			"testdata/ListTemplateGenerated.golden.proto"},
	}
	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			oIntface, err := ioutil.TempFile("", v.name)
			if err != nil {
				t.Fatal(err)
			}
			oTmpl, err := ioutil.TempFile("", v.name)
			if err != nil {
				t.Fatal(err)
			}

			defer func() {
				if !t.Failed() {
					if removeErr := os.Remove(oIntface.Name()); removeErr != nil {
						t.Logf("Could not remove temporary file %s: %v", oIntface.Name(), removeErr)
					}
					if removeErr := os.Remove(oTmpl.Name()); removeErr != nil {
						t.Logf("Could not remove temporary file %s: %v", oTmpl.Name(), removeErr)
					}
				}
			}()

			g := Generator{OutInterfacePath: oIntface.Name(), OAugmentedTmplPath: oTmpl.Name(), ImptMap: importmap}

			if err := g.Generate(v.descriptor); err != nil {
				t.Fatalf("Generate(%s) produced an error: %v", v.descriptor, err)
			}

			if same := fileCompare(oIntface.Name(), v.wantIntFace, t.Errorf, false); !same {
				t.Error("Files were not the same.")
			}

			if same := fileCompare(oTmpl.Name(), v.wantProto, t.Errorf, true); !same {
				t.Error("Files were not the same.")
			}
		})
	}
}

func TestGenerator_GenerateErrors(t *testing.T) {
	file, err := ioutil.TempFile("", "error_file")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if removeErr := os.Remove(file.Name()); removeErr != nil {
			t.Logf("Could not remove temporary file %s: %v", file.Name(), removeErr)
		}
	}()

	g := Generator{OutInterfacePath: file.Name()}
	err = g.Generate("testdata/error_template.descriptor_set")
	if err == nil {
		t.Fatalf("Generate(%s) should have produced an error", "testdata/error_template.descriptor_set")
	}
	b, fileErr := ioutil.ReadFile("testdata/ErrorTemplate.baseline")
	if fileErr != nil {
		t.Fatalf("Could not read baseline file: %v", err)
	}
	want := fmt.Sprintf("%s", b)
	got := err.Error()
	if got != want {
		t.Fatalf("Generate(%s) => '%s'\nwanted: '%s'", "testdata/error_template.descriptor_set", got, want)
	}
}

const chunkSize = 64000

func fileCompare(actual, want string, logf logFn, skipSpaces bool) bool {
	f1, err := os.Open(actual)
	if err != nil {
		logf("could not open file: %v", err)
		return false
	}

	f2, err := os.Open(want)
	if err != nil {
		logf("could not open file: %v", err)
		return false
	}

	for {
		b1 := make([]byte, chunkSize)
		_, err1 := f1.Read(b1)
		b1 = bytes.Trim(b1, "\x00")
		b2 := make([]byte, chunkSize)
		_, err2 := f2.Read(b2)
		b2 = bytes.Trim(b2, "\x00")

		b1ToCmp := b1
		b2ToCmp := b2

		if skipSpaces {
			b1ToCmp = bytes.Replace(b1ToCmp, []byte(" "), []byte(""), -1)
			b1ToCmp = bytes.Replace(b1ToCmp, []byte("\n"), []byte(""), -1)
			b2ToCmp = bytes.Replace(b2ToCmp, []byte(" "), []byte(""), -1)
			b2ToCmp = bytes.Replace(b2ToCmp, []byte("\n"), []byte(""), -1)
		}

		if err1 == io.EOF && err2 == io.EOF {
			return true
		}

		if err1 != nil || err2 != nil {
			return false
		}
		if !bytes.Equal(b1ToCmp, b2ToCmp) {
			logf("bytes don't match (sizes: %d, %d):\n%s\nNOT EQUALS\n%s.\n"+
				"Got file content:\n%s\nWant file content:\n%s\n",
				len(b1ToCmp), len(b2ToCmp), string(b1ToCmp), string(b2ToCmp), string(b1), string(b2))
			return false
		}
	}
}
