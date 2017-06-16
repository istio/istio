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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"testing"
)

func TestWellKnownTemplate(t *testing.T) {
	tests := []struct {
		src      string
		baseline string
	}{
		{"testdata/MetricTemplate.proto", "testdata/MetricTemplateProcessorInterface.go.baseline"},
		{"testdata/MetricTemplate.proto", "testdata/MetricTemplateProcessorInterface.go.baseline"},
		{"testdata/QuotaTemplate.proto", "testdata/QuotaTemplateProcessorInterface.go.baseline"},
		{"testdata/LogTemplate.proto", "testdata/LogTemplateProcessorInterface.go.baseline"},
		{"testdata/ListTemplate.proto", "testdata/ListTemplateProcessorInterface.go.baseline"},
		{"testdata/NestedMessage.proto", "testdata/NestedMessageProcessor.go.baseline"},
		{"testdata/ErrorTemplate.proto", "testdata/ErrorTemplate.baseline"},
	}
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.src), func(t *testing.T) {
			outDir := path.Join("testdata", getBaseFileNameWithoutExt(t.Name()))
			_ = os.RemoveAll(outDir)
			_ = os.MkdirAll(outDir, os.ModePerm)

			outFDS := path.Join(outDir, "outFDS.pb")
			defer removeDir(outFDS)
			err := generteFDSFileHacky(tt.src, outFDS)
			if err != nil {
				t.Fatalf("Unable to generate file descriptor set %v", err)
			}

			outFilePath := path.Join(outDir, "Processor.go")
			generator := Generator{OutFilePath: outFilePath, ImportMapping: map[string]string{
				"mixer/v1/config/descriptor/value_type.proto":                         "istio.io/api/mixer/v1/config/descriptor",
				"mixer/tools/codegen/pkg/template_extension/TemplateExtensions.proto": "istio.io/mixer/tools/codegen/pkg/template_extension",
				"google/protobuf/duration.proto":                                      "github.com/golang/protobuf/ptypes/duration",
			}}
			if err = generator.Generate(outFDS); err == nil {
				/*
					Below commented code is for testing if the generated code compiles correctly. Currently to test that, I have to
					run protoc separately copy the generated pb.go in the tmp output folder (doing it via a separate script),
					then uncomment the code and run the test. Need to find a cleaner automated way.
				*/
				// Generate *.pb.go file for the template and copy it into the outDir
				err = generteGoPbFileForTmpl(tt.src, outDir)
				if err != nil {
					t.Errorf("Unable to generate go file for %s: %v", tt.src, err)
				}
				protocCmd := []string{
					"build",
				}
				cmd := exec.Command("go", protocCmd...)
				cmd.Dir = outDir
				cmd.Stderr = os.Stderr // For debugging
				err = cmd.Run()
				if err != nil {
					t.FailNow()
					return
				}
			} else {
				_ = ioutil.WriteFile(outFilePath, []byte(err.Error()), 0644)
			}

			diffCmd := exec.Command("diff", outFilePath, tt.baseline, "--ignore-all-space")
			diffCmd.Stdout = os.Stdout
			diffCmd.Stderr = os.Stderr
			err = diffCmd.Run()
			if err != nil {
				t.Fatalf("Diff failed: %+v. Expected output is located at %s", err, outFilePath)
				return
			}

			// if the test succeeded, clean up
			_ = os.RemoveAll(outDir)
		})
	}
}

func removeDir(dir string) {
	_ = os.Remove(dir)
}

// TODO: This is blocking the test to be enabled from Bazel.
func generteFDSFileHacky(protoFile string, outputFDSFile string) error {

	// HACK HACK. Depending on dir structure is super fragile.
	// Explore how to generate File Descriptor set in a better way.
	protocCmd := []string{
		path.Join("mixer/tools/codegen/pkg/interfacegen", protoFile),
		"-o",
		path.Join("mixer/tools/codegen/pkg/interfacegen", outputFDSFile),
		"-I=.",
		"-I=api",
		"--include_imports",
	}
	cmd := exec.Command("protoc", protocCmd...)
	dir := path.Join(os.Getenv("GOPATH"), "src/istio.io")
	cmd.Dir = dir
	cmd.Stderr = os.Stderr // For debugging
	err := cmd.Run()
	return err
}

// TODO: This is blocking the test to be enabled from Bazel.
func generteGoPbFileForTmpl(protoFile string, outDir string) error {

	// HACK HACK. Depending on dir structure is super fragile.
	// Explore how to generate File Descriptor set in a better way.
	protocCmd := []string{
		path.Join("mixer/tools/codegen/pkg/interfacegen", protoFile),
		"--go_out=Mgoogle/protobuf/duration.proto=github.com/golang/protobuf/ptypes/duration,Mmixer/v1/" +
			"config/descriptor/value_type.proto=istio.io/api/mixer/v1/config/descriptor," +
			"Mgoogle/protobuf/descriptor.proto=github.com/golang/protobuf/protoc-gen-go/descriptor," +
			"Mgoogle/protobuf/struct.proto=github.com/golang/protobuf/ptypes/struct," +
			"Mmixer/tools/codegen/pkg/template_extension/TemplateExtensions.proto=istio.io/mixer/tools/codegen/pkg/template_extension:.",
		"-I=.",
		"-I=api",
	}

	cmd := exec.Command("protoc", protocCmd...)
	dir := path.Join(os.Getenv("GOPATH"), "src/istio.io")
	cmd.Dir = dir
	cmd.Stderr = os.Stderr // For debugging
	err := cmd.Run()

	if err != nil {
		return err
	}

	// Copy to the outDir
	genGoFilePath := path.Join(path.Dir(protoFile), getBaseFileNameWithoutExt(protoFile)+".pb.go")
	// first do the magic replacement
	sedCmd := exec.Command("sed", "-i", "-e", "s/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g", genGoFilePath)
	sedCmd.Stdout = os.Stdout
	sedCmd.Stderr = os.Stderr
	err = sedCmd.Run()
	if err != nil {
		return err
	}

	mvCmd := exec.Command("mv", genGoFilePath, outDir)
	mvCmd.Stdout = os.Stdout
	mvCmd.Stderr = os.Stderr

	return mvCmd.Run()
}

func getBaseFileNameWithoutExt(filePath string) string {
	tmp := filepath.Base(filePath)
	return tmp[0 : len(tmp)-len(filepath.Ext(tmp))]
}
