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
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/report/template.proto -otestdata/report/template.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/quota/template.proto -otestdata/quota/template.descriptor -I.
//go:generate $REPO_ROOT/bin/protoc.sh --include_imports --include_source_info testdata/error/template.proto -otestdata/error/template.descriptor -I.
package cmd

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"
)

type logFn func(string, ...interface{})

// TestGenerator_Generate uses the outputs file descriptors generated via bazel
// and compares them against the golden files.
func TestGenerator_Generate(t *testing.T) {
	tests := []struct {
		name, descriptor, wantIntFace, wantProto string
	}{
		{"Report", "testdata/report/template.descriptor",
			"testdata/report/template_handler.gen.go.golden",
			"testdata/report/template_instance.proto.golden"},
		{"Quota", "testdata/quota/template.descriptor",
			"testdata/quota/template_handler.gen.go.golden",
			"testdata/quota/template_instance.proto.golden"},
		{"Check", "testdata/check/template.descriptor",
			"testdata/check/template_handler.gen.go.golden",
			"testdata/check/template_instance.proto.golden"},
		{"APA", "testdata/apa/template.descriptor",
			"testdata/apa/template_handler.gen.go.golden",
			"testdata/apa/template_instance.proto.golden"},
	}
	for _, v := range tests {
		t.Run(v.name, func(tt *testing.T) {
			tmpDir := path.Join(os.TempDir(), v.name)
			_ = os.MkdirAll(tmpDir, os.ModeDir|os.ModePerm)
			oIntface, err := os.Create(path.Join(tmpDir, path.Base(v.wantIntFace)))
			if err != nil {
				tt.Fatal(err)
			}
			oTmpl, err := os.Create(path.Join(tmpDir, path.Base(v.wantProto)))
			if err != nil {
				tt.Fatal(err)
			}

			defer func() {
				if !tt.Failed() {
					if removeErr := os.Remove(oIntface.Name()); removeErr != nil {
						tt.Logf("Could not remove temporary file %s: %v", oIntface.Name(), removeErr)
					}
					if removeErr := os.Remove(oTmpl.Name()); removeErr != nil {
						tt.Logf("Could not remove temporary file %s: %v", oTmpl.Name(), removeErr)
					}
				}
			}()

			args := []string{"api", "-t", v.descriptor, "--go_out", oIntface.Name(), "--proto_out", oTmpl.Name()}

			args = append(args, "-m", "policy/v1beta1/value_type.proto:istio.io/api/policy/v1beta1",
				"-m", "gogoproto/gogo.proto:github.com/gogo/protobuf/gogoproto",
				"-m", "mixer/adapter/model/v1beta1/extensions.proto:istio.io/api/mixer/adapter/model/v1beta1",
				"-m", "google/protobuf/duration.proto:github.com/gogo/protobuf/types",
			)

			root := GetRootCmd(args,
				func(format string, a ...interface{}) {
				},
				func(format string, a ...interface{}) {
					tt.Fatalf("want error 'nil'; got '%s'", fmt.Sprintf(format, a...))
				})

			_ = root.Execute()

			if same := fileCompare(oIntface.Name(), v.wantIntFace, tt.Errorf, false); !same {
				tt.Errorf("File %s does not match baseline %s.", oIntface.Name(), v.wantIntFace)
			}

			if same := fileCompare(oTmpl.Name(), v.wantProto, tt.Errorf, true); !same {
				tt.Errorf("File %s does not match baseline %s.", oTmpl.Name(), v.wantProto)
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
	err = g.Generate("testdata/error/template.descriptor")
	if err == nil {
		t.Fatalf("Generate(%s) should have produced an error", "testdata/error/template.descriptor")
	}
	b, fileErr := ioutil.ReadFile("testdata/error/template.baseline")
	if fileErr != nil {
		t.Fatalf("Could not read baseline file: %v", err)
	}
	want := fmt.Sprintf("%s", b)
	got := err.Error()
	if got != want {
		t.Fatalf("Generate(%s) => '%s'\nwanted: '%s'", "testdata/error/template.descriptor", got, want)
	}
}

func TestGeneratorWithBadFdsPath(t *testing.T) {
	g := Generator{}
	err := g.Generate("bad file path")
	validateHasError(t, err, "no such file")
}

func TestGeneratorWithBadFds(t *testing.T) {
	g := Generator{}
	err := g.Generate("testdata/check/template.proto")
	validateHasError(t, err, "as a FileDescriptorSetProto")
}

func TestGeneratorBadInterfaceTmpl(t *testing.T) {
	g := Generator{}
	err := g.generateInternal("testdata/report/template.descriptor", "{{foo}} bad tmpl", augmentedProtoTmpl)
	validateHasError(t, err, "cannot load template")
}

func TestGeneratorBadAugmentedProtoTmpl(t *testing.T) {
	g := Generator{}
	err := g.generateInternal("testdata/report/template.descriptor", interfaceTemplate, "{{foo}} bad tmpl")
	validateHasError(t, err, "cannot load template")
}

func TestGeneratorBadOutputAugProto(t *testing.T) {
	g := Generator{OutInterfacePath: "out1", OAugmentedTmplPath: ""}
	defer func() { os.Remove("out1") }()
	err := g.Generate("testdata/report/template.descriptor")
	validateHasError(t, err, "no such file")
}

func TestGeneratorBadOutputInstanceInterface(t *testing.T) {
	g := Generator{OutInterfacePath: "", OAugmentedTmplPath: "out1"}
	defer func() { os.Remove("out1") }()
	err := g.Generate("testdata/report/template.descriptor")
	validateHasError(t, err, "no such file")
}

func TestGeneratorHandlerInterfaceBadModel(t *testing.T) {
	g := Generator{}
	_, err := g.getInterfaceGoContent(nil, interfaceTemplate)
	validateHasError(t, err, "cannot execute the template")
}

func TestGeneratorAugmentedProtoBadModel(t *testing.T) {
	g := Generator{}
	_, err := g.getAugmentedProtoContent(nil, "", augmentedProtoTmpl)
	validateHasError(t, err, "cannot execute the template")
}

func TestGeneratorCannotFormat(t *testing.T) {
	g := Generator{}
	err := g.generateInternal("testdata/report/template.descriptor", ".. bad format", augmentedProtoTmpl)
	validateHasError(t, err, "could not format")
}

func TestGeneratorCannotFixImport(t *testing.T) {
	g := Generator{}
	err := g.generateInternal("testdata/report/template.descriptor", "badtmpl_cannot_fix_import", augmentedProtoTmpl)
	validateHasError(t, err, "could not fix imports")
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

func validateHasError(t *testing.T, err error, want string) {
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want error with msg \"%s\"; got %v", want, err)
	}
}
