// Copyright 2018 Istio Authors
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

//go:generate protoc testdata/foo.proto -otestdata/foo.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.
package cmd

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

type adapterCmdTestdata struct {
	descriptorContent string
	description       string
	name              string
	sessionBased      bool
	templates         []string
	wantCfg           string
}

func TestAdapterCmd(t *testing.T) {
	for i, td := range []adapterCmdTestdata{
		{
			descriptorContent: "some text",
			description:       "mydescription",
			name:              "myresName",
			sessionBased:      false,
			templates:         []string{"foo.bar", "foo1.bar1"},
			wantCfg: `# this config is created through command
# mixgen adapter -n myresName -c tempDescriptorFile --description mydescription -s=false -t foo.bar -t foo1.bar1
apiVersion: "config.istio.io/v1alpha2"
kind: adapter
metadata:
  name: myresName
  namespace: istio-system
spec:
  description: mydescription
  session_based: false
  templates:
  - foo.bar
  - foo1.bar1
  config: c29tZSB0ZXh0
---
`,
		},
		{
			descriptorContent: "some text",
			description:       "mydescription",
			name:              "myresName",
			sessionBased:      true,
			wantCfg: `# this config is created through command
# mixgen adapter -n myresName -c tempDescriptorFile --description mydescription -s=true
apiVersion: "config.istio.io/v1alpha2"
kind: adapter
metadata:
  name: myresName
  namespace: istio-system
spec:
  description: mydescription
  session_based: true
  templates:
  config: c29tZSB0ZXh0
---
`,
		},
	} {
		file, _ := os.Create("tempDescriptorFile")
		defer func() {
			if removeErr := os.Remove(file.Name()); removeErr != nil {
				t.Logf("could not remove temporary file %s: %v", file.Name(), removeErr)
			}
		}()

		t.Run(fmt.Sprintf("%d", i), func(tt *testing.T) {
			_, _ = file.WriteString(td.descriptorContent)
			fName := file.Name()
			args := getArgs(td, fName)
			gotCfg := ""
			root := GetRootCmd(args,
				func(format string, a ...interface{}) {
					gotCfg = fmt.Sprintf(format, a...)
				},
				func(format string, a ...interface{}) {
					tt.Fatalf("want error 'nil'; got '%s'", fmt.Sprintf(format, a...))
				})

			_ = root.Execute()

			if gotCfg != td.wantCfg {
				tt.Errorf("want :\n%v\ngot :\n%v", td.wantCfg, gotCfg)
			}
		})
	}
}

func TestAdapterCmd_NoInputFile(t *testing.T) {
	var gotError string
	cmd := GetRootCmd([]string{"adapter"},
		func(format string, a ...interface{}) {},
		func(format string, a ...interface{}) {
			gotError = fmt.Sprintf(format, a...)
			if !strings.Contains(gotError, "unable to read") {
				t.Fatalf("want error 'unable to read'; got '%s'", gotError)
			}
		})
	_ = cmd.Execute()
	if gotError == "" {
		t.Errorf("want error; got nil")
	}
}

func getArgs(td adapterCmdTestdata, fName string) []string {
	args := []string{
		"adapter",
		"-n", td.name,
		"-c", fName,
		"--description", td.description,
		"-s=" + fmt.Sprintf("%v", td.sessionBased),
	}
	for _, tmpl := range td.templates {
		args = append(args, fmt.Sprintf("-t %s", tmpl))
	}
	return args
}
