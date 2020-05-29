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

// nolint
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/apa/template.proto -otestdata/apa/template.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/check/template.proto -otestdata/check/template.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/quota/template.proto -otestdata/quota/template.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/report1/template.proto -otestdata/report1/template.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/report2/template.proto -otestdata/report2/template.descriptor -I.
package bootstrapgen

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"testing"

	"istio.io/istio/mixer/pkg/protobuf/descriptor"
	"istio.io/istio/mixer/tools/codegen/pkg/bootstrapgen/template"
	"istio.io/istio/mixer/tools/codegen/pkg/modelgen"
	"istio.io/istio/pilot/test/util"
)

type logFn func(string, ...interface{})

// TestGenerator_Generate uses the outputs file descriptors generated via bazel
// and compares them against the golden files.
func TestGenerator_Generate(t *testing.T) {
	importmap := map[string]string{
		"policy/v1beta1/value_type.proto":              "istio.io/api/policy/v1beta1",
		"mixer/adapter/model/v1beta1/extensions.proto": "istio.io/api/mixer/adapter/model/v1beta1",
		"gogoproto/gogo.proto":                         "github.com/gogo/protobuf/gogoproto",
		"google/protobuf/duration.proto":               "github.com/gogo/protobuf/types",
	}

	tests := []struct {
		name     string
		fdsFiles map[string]string // FDS and their package import paths
		want     string
	}{
		{"AllTemplates", map[string]string{
			"testdata/check/template.descriptor":   "istio.io/istio/mixer/template/list",
			"testdata/report2/template.descriptor": "istio.io/istio/mixer/template/metric",
			"testdata/quota/template.descriptor":   "istio.io/istio/mixer/template/quota",
			"testdata/apa/template.descriptor":     "istio.io/istio/mixer/template/apa",
			"testdata/report1/template.descriptor": "istio.io/istio/mixer/template/log"},
			"testdata/template.gen.go.golden"},
	}
	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			testTmpDir := path.Join(os.TempDir(), "bootstrapTemplateTest")
			_ = os.MkdirAll(testTmpDir, os.ModeDir|os.ModePerm)
			outFile, err := os.Create(path.Join(testTmpDir, path.Base(v.want)))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if !t.Failed() {
					if removeErr := os.RemoveAll(testTmpDir); removeErr != nil {
						t.Logf("Could not remove temporary folder %s: %v", testTmpDir, removeErr)

					}
				} else {
					t.Logf("Generated data is located at '%s'", testTmpDir)
				}
			}()

			g := Generator{OutFilePath: outFile.Name(), ImportMapping: importmap}
			if err := g.Generate(v.fdsFiles); err != nil {
				t.Fatalf("Generate(%s) produced an error: %v", v.fdsFiles, err)
			}

			if same := fileCompare(outFile.Name(), v.want, t.Errorf); !same {
				t.Errorf("Files %v and %v were not the same.", outFile.Name(), v.want)
			}
		})
	}
}

func TestGeneratorWithBadFds(t *testing.T) {
	g := Generator{OutFilePath: ""}
	err := g.Generate(
		// testdata/check/template.proto" is not a fds
		map[string]string{"testdata/check/template.proto": "istio.io/istio/mixer/template/list"},
	)
	validateHasError(t, err, "as a FileDescriptorSetProto")
}

func TestGeneratorWithBadFdsPath(t *testing.T) {
	g := Generator{OutFilePath: ""}
	err := g.Generate(map[string]string{"bad file path": "bad file path"})
	validateHasError(t, err, "no such file")
}

func TestGeneratorWithBadOutFilePath(t *testing.T) {
	g := Generator{OutFilePath: "bad/bad"}
	err := g.Generate(
		map[string]string{"testdata/check/template.descriptor": "istio.io/istio/mixer/template/list"})
	validateHasError(t, err, "no such file or directory")
}

func TestGeneratorBadTmpl(t *testing.T) {
	g := Generator{OutFilePath: "bad/bad"}
	err := g.generateInternal(
		map[string]string{"testdata/check/template.descriptor": "istio.io/istio/mixer/template/list"},
		"{{getResourcMessageInterfaceParamTypeName bad}}", modelgen.Create)
	validateHasError(t, err, "cannot load template")
}

func TestGeneratorBadModel(t *testing.T) {
	testTmpDir := path.Join(os.TempDir(), "TestGeneratorBadModel")
	_ = os.MkdirAll(testTmpDir, os.ModeDir|os.ModePerm)
	outFile, err := os.Create(path.Join(testTmpDir, "o.out"))
	g := Generator{OutFilePath: outFile.Name()}
	if err != nil {
		t.Fatal(err)
	}
	err = g.generateInternal(
		map[string]string{"testdata/check/template.descriptor": "istio.io/istio/mixer/template/list"},
		template.BootstrapTemplate, func(parser *descriptor.FileDescriptorSetParser) (*modelgen.Model, error) {
			return nil, fmt.Errorf("bad model")
		})
	validateHasError(t, err, "bad model")
}

func TestGeneratorNilModel(t *testing.T) {
	testTmpDir := path.Join(os.TempDir(), "TestGeneratorNilModel")
	_ = os.MkdirAll(testTmpDir, os.ModeDir|os.ModePerm)
	outFile, err := os.Create(path.Join(testTmpDir, "o.out"))
	g := Generator{OutFilePath: outFile.Name()}
	if err != nil {
		t.Fatal(err)
	}
	err = g.generateInternal(
		map[string]string{"testdata/check/template.descriptor": "istio.io/istio/mixer/template/list"},
		template.BootstrapTemplate, func(parser *descriptor.FileDescriptorSetParser) (*modelgen.Model, error) { return nil, nil })
	validateHasError(t, err, "cannot execute the template")
}

func TestGeneratorCannotFormat(t *testing.T) {
	testTmpDir := path.Join(os.TempDir(), "TestGeneratorNilModel")
	_ = os.MkdirAll(testTmpDir, os.ModeDir|os.ModePerm)
	outFile, err := os.Create(path.Join(testTmpDir, "o.out"))
	g := Generator{OutFilePath: outFile.Name()}
	if err != nil {
		t.Fatal(err)
	}
	err = g.generateInternal(
		map[string]string{"testdata/check/template.descriptor": "istio.io/istio/mixer/template/list"},
		". . badtmpl_cannot_be_formatted", modelgen.Create)
	validateHasError(t, err, "could not format")
}

func TestGeneratorCannotFixImport(t *testing.T) {
	testTmpDir := path.Join(os.TempDir(), "TestGeneratorNilModel")
	_ = os.MkdirAll(testTmpDir, os.ModeDir|os.ModePerm)
	outFile, err := os.Create(path.Join(testTmpDir, "o.out"))
	g := Generator{OutFilePath: outFile.Name()}
	if err != nil {
		t.Fatal(err)
	}
	err = g.generateInternal(
		map[string]string{"testdata/check/template.descriptor": "istio.io/istio/mixer/template/list"},
		"badtmpl_cannot_fix_import", modelgen.Create)
	validateHasError(t, err, "could not fix imports")
}

const chunkSize = 64000

func fileCompare(file1, file2 string, logf logFn) bool {
	f1, err := os.Open(file1)
	if err != nil {
		logf("could not open file: %v", err)
		return false
	}

	f2, err := os.Open(file2)
	if err != nil {
		logf("could not open file: %v", err)
		return false
	}

	for {
		b1 := make([]byte, chunkSize)
		s1, err1 := f1.Read(b1)

		b2 := make([]byte, chunkSize)
		s2, err2 := f2.Read(b2)

		if err1 == io.EOF && err2 == io.EOF {
			return true
		}

		if err1 != nil || err2 != nil {
			return false
		}

		if !bytes.Equal(b1, b2) {
			logf("bytes don't match (sizes: %d, %d):\n%v", s1, s2, util.Compare(b1, b2))
			return false
		}
	}
}

func validateHasError(t *testing.T, err error, want string) {
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want error with msg \"%s\"; got %v", want, err)
	}
}
