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

//go:generate $GOPATH/src/istio.io/istio/bin/protoc.sh testdata/simple/foo.proto -otestdata/simple/foo.descriptor
// --include_imports -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.
//go:generate $GOPATH/src/istio.io/istio/bin/protoc.sh testdata/simple/foo.proto
// -otestdata/simple/foo_without_imports.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.
package cmd

import (
	"fmt"
	"strings"
	"testing"
)

type adapterCmdTestdata struct {
	configFile   string
	description  string
	name         string
	sessionBased bool
	templates    []string
	wantCfg      string
	wantErr      string
}

func TestAdapterCmd(t *testing.T) {
	for i, td := range []adapterCmdTestdata{
		{
			description:  "mydescription",
			configFile:   "testdata/simple/foo.descriptor",
			name:         "myresName",
			sessionBased: false,
			templates:    []string{"foo.bar", "foo1.bar1"},
			wantCfg: `# this config is created through command
# mixgen adapter -n myresName -c testdata/simple/foo.descriptor --description mydescription -s=false -t=foo.bar -t=foo1.bar1
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
  config: CvMBCh5nb29nbGUvcHJvdG9idWYvZHVyYXRpb24ucHJvdG8SD2dvb2dsZS5wcm90b2J1ZiI6CghEdXJhdGlvbhIYCgdzZWNvbmRzGAEgASgDUgdzZWNvbmRzEhQKBW5hbm9zGAIgASgFUgVuYW5vc0J8ChNjb20uZ29vZ2xlLnByb3RvYnVmQg1EdXJhdGlvblByb3RvUAFaKmdpdGh1Yi5jb20vZ29sYW5nL3Byb3RvYnVmL3B0eXBlcy9kdXJhdGlvbvgBAaICA0dQQqoCHkdvb2dsZS5Qcm90b2J1Zi5XZWxsS25vd25UeXBlc2IGcHJvdG8zCvcBCh9nb29nbGUvcHJvdG9idWYvdGltZXN0YW1wLnByb3RvEg9nb29nbGUucHJvdG9idWYiOwoJVGltZXN0YW1wEhgKB3NlY29uZHMYASABKANSB3NlY29uZHMSFAoFbmFub3MYAiABKAVSBW5hbm9zQn4KE2NvbS5nb29nbGUucHJvdG9idWZCDlRpbWVzdGFtcFByb3RvUAFaK2dpdGh1Yi5jb20vZ29sYW5nL3Byb3RvYnVmL3B0eXBlcy90aW1lc3RhbXD4AQGiAgNHUEKqAh5Hb29nbGUuUHJvdG9idWYuV2VsbEtub3duVHlwZXNiBnByb3RvMwr2BwoZcG9saWN5L3YxYmV0YTEvdHlwZS5wcm90bxIUaXN0aW8ucG9saWN5LnYxYmV0YTEaHmdvb2dsZS9wcm90b2J1Zi9kdXJhdGlvbi5wcm90bxofZ29vZ2xlL3Byb3RvYnVmL3RpbWVzdGFtcC5wcm90byLXBAoFVmFsdWUSIwoMc3RyaW5nX3ZhbHVlGAEgASgJSABSC3N0cmluZ1ZhbHVlEiEKC2ludDY0X3ZhbHVlGAIgASgDSABSCmludDY0VmFsdWUSIwoMZG91YmxlX3ZhbHVlGAMgASgBSABSC2RvdWJsZVZhbHVlEh8KCmJvb2xfdmFsdWUYBCABKAhIAFIJYm9vbFZhbHVlEksKEGlwX2FkZHJlc3NfdmFsdWUYBSABKAsyHy5pc3Rpby5wb2xpY3kudjFiZXRhMS5JUEFkZHJlc3NIAFIOaXBBZGRyZXNzVmFsdWUSSgoPdGltZXN0YW1wX3ZhbHVlGAYgASgLMh8uaXN0aW8ucG9saWN5LnYxYmV0YTEuVGltZVN0YW1wSABSDnRpbWVzdGFtcFZhbHVlEkcKDmR1cmF0aW9uX3ZhbHVlGAcgASgLMh4uaXN0aW8ucG9saWN5LnYxYmV0YTEuRHVyYXRpb25IAFINZHVyYXRpb25WYWx1ZRJUChNlbWFpbF9hZGRyZXNzX3ZhbHVlGAggASgLMiIuaXN0aW8ucG9saWN5LnYxYmV0YTEuRW1haWxBZGRyZXNzSABSEWVtYWlsQWRkcmVzc1ZhbHVlEkUKDmRuc19uYW1lX3ZhbHVlGAkgASgLMh0uaXN0aW8ucG9saWN5LnYxYmV0YTEuRE5TTmFtZUgAUgxkbnNOYW1lVmFsdWUSOAoJdXJpX3ZhbHVlGAogASgLMhkuaXN0aW8ucG9saWN5LnYxYmV0YTEuVXJpSABSCHVyaVZhbHVlQgcKBXZhbHVlIiEKCUlQQWRkcmVzcxIUCgV2YWx1ZRgBIAEoDFIFdmFsdWUiOwoIRHVyYXRpb24SLwoFdmFsdWUYASABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25SBXZhbHVlIj0KCVRpbWVTdGFtcBIwCgV2YWx1ZRgBIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSBXZhbHVlIh8KB0ROU05hbWUSFAoFdmFsdWUYASABKAlSBXZhbHVlIiQKDEVtYWlsQWRkcmVzcxIUCgV2YWx1ZRgBIAEoCVIFdmFsdWUiGwoDVXJpEhQKBXZhbHVlGAEgASgJUgV2YWx1ZUIdWhtpc3Rpby5pby9hcGkvcG9saWN5L3YxYmV0YTFiBnByb3RvMwqNAQoZdGVzdGRhdGEvc2ltcGxlL2Zvby5wcm90bxIZaXN0aW8ubWl4ZXIuYWRhcHRlci5xdW90YRoZcG9saWN5L3YxYmV0YTEvdHlwZS5wcm90byIyCgFhEi0KA3ZhbBgBIAEoCzIbLmlzdGlvLnBvbGljeS52MWJldGExLlZhbHVlUgN2YWxiBnByb3RvMw==
---
`,
		},
		{
			description:  "mydescription",
			configFile:   "testdata/simple/foo.descriptor",
			name:         "myresName",
			sessionBased: true,
			wantCfg: `# this config is created through command
# mixgen adapter -n myresName -c testdata/simple/foo.descriptor --description mydescription -s=true
apiVersion: "config.istio.io/v1alpha2"
kind: adapter
metadata:
  name: myresName
  namespace: istio-system
spec:
  description: mydescription
  session_based: true
  templates:
  config: CvMBCh5nb29nbGUvcHJvdG9idWYvZHVyYXRpb24ucHJvdG8SD2dvb2dsZS5wcm90b2J1ZiI6CghEdXJhdGlvbhIYCgdzZWNvbmRzGAEgASgDUgdzZWNvbmRzEhQKBW5hbm9zGAIgASgFUgVuYW5vc0J8ChNjb20uZ29vZ2xlLnByb3RvYnVmQg1EdXJhdGlvblByb3RvUAFaKmdpdGh1Yi5jb20vZ29sYW5nL3Byb3RvYnVmL3B0eXBlcy9kdXJhdGlvbvgBAaICA0dQQqoCHkdvb2dsZS5Qcm90b2J1Zi5XZWxsS25vd25UeXBlc2IGcHJvdG8zCvcBCh9nb29nbGUvcHJvdG9idWYvdGltZXN0YW1wLnByb3RvEg9nb29nbGUucHJvdG9idWYiOwoJVGltZXN0YW1wEhgKB3NlY29uZHMYASABKANSB3NlY29uZHMSFAoFbmFub3MYAiABKAVSBW5hbm9zQn4KE2NvbS5nb29nbGUucHJvdG9idWZCDlRpbWVzdGFtcFByb3RvUAFaK2dpdGh1Yi5jb20vZ29sYW5nL3Byb3RvYnVmL3B0eXBlcy90aW1lc3RhbXD4AQGiAgNHUEKqAh5Hb29nbGUuUHJvdG9idWYuV2VsbEtub3duVHlwZXNiBnByb3RvMwr2BwoZcG9saWN5L3YxYmV0YTEvdHlwZS5wcm90bxIUaXN0aW8ucG9saWN5LnYxYmV0YTEaHmdvb2dsZS9wcm90b2J1Zi9kdXJhdGlvbi5wcm90bxofZ29vZ2xlL3Byb3RvYnVmL3RpbWVzdGFtcC5wcm90byLXBAoFVmFsdWUSIwoMc3RyaW5nX3ZhbHVlGAEgASgJSABSC3N0cmluZ1ZhbHVlEiEKC2ludDY0X3ZhbHVlGAIgASgDSABSCmludDY0VmFsdWUSIwoMZG91YmxlX3ZhbHVlGAMgASgBSABSC2RvdWJsZVZhbHVlEh8KCmJvb2xfdmFsdWUYBCABKAhIAFIJYm9vbFZhbHVlEksKEGlwX2FkZHJlc3NfdmFsdWUYBSABKAsyHy5pc3Rpby5wb2xpY3kudjFiZXRhMS5JUEFkZHJlc3NIAFIOaXBBZGRyZXNzVmFsdWUSSgoPdGltZXN0YW1wX3ZhbHVlGAYgASgLMh8uaXN0aW8ucG9saWN5LnYxYmV0YTEuVGltZVN0YW1wSABSDnRpbWVzdGFtcFZhbHVlEkcKDmR1cmF0aW9uX3ZhbHVlGAcgASgLMh4uaXN0aW8ucG9saWN5LnYxYmV0YTEuRHVyYXRpb25IAFINZHVyYXRpb25WYWx1ZRJUChNlbWFpbF9hZGRyZXNzX3ZhbHVlGAggASgLMiIuaXN0aW8ucG9saWN5LnYxYmV0YTEuRW1haWxBZGRyZXNzSABSEWVtYWlsQWRkcmVzc1ZhbHVlEkUKDmRuc19uYW1lX3ZhbHVlGAkgASgLMh0uaXN0aW8ucG9saWN5LnYxYmV0YTEuRE5TTmFtZUgAUgxkbnNOYW1lVmFsdWUSOAoJdXJpX3ZhbHVlGAogASgLMhkuaXN0aW8ucG9saWN5LnYxYmV0YTEuVXJpSABSCHVyaVZhbHVlQgcKBXZhbHVlIiEKCUlQQWRkcmVzcxIUCgV2YWx1ZRgBIAEoDFIFdmFsdWUiOwoIRHVyYXRpb24SLwoFdmFsdWUYASABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb25SBXZhbHVlIj0KCVRpbWVTdGFtcBIwCgV2YWx1ZRgBIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSBXZhbHVlIh8KB0ROU05hbWUSFAoFdmFsdWUYASABKAlSBXZhbHVlIiQKDEVtYWlsQWRkcmVzcxIUCgV2YWx1ZRgBIAEoCVIFdmFsdWUiGwoDVXJpEhQKBXZhbHVlGAEgASgJUgV2YWx1ZUIdWhtpc3Rpby5pby9hcGkvcG9saWN5L3YxYmV0YTFiBnByb3RvMwqNAQoZdGVzdGRhdGEvc2ltcGxlL2Zvby5wcm90bxIZaXN0aW8ubWl4ZXIuYWRhcHRlci5xdW90YRoZcG9saWN5L3YxYmV0YTEvdHlwZS5wcm90byIyCgFhEi0KA3ZhbBgBIAEoCzIbLmlzdGlvLnBvbGljeS52MWJldGExLlZhbHVlUgN2YWxiBnByb3RvMw==
---
`,
		},
		{
			configFile: "testdata/simple/foo_without_imports.descriptor",
			wantErr: "config in invalid: the file descriptor set was created without including imports. Please run " +
				"protoc with `--include_imports` flag",
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(tt *testing.T) {
			args := getArgs(td, td.configFile)
			gotCfg := ""
			root := GetRootCmd(args,
				func(format string, a ...interface{}) {
					gotCfg = fmt.Sprintf(format, a...)
				},
				func(format string, a ...interface{}) {
					gotError := fmt.Sprintf(format, a...)
					if td.wantErr == "" {
						tt.Fatalf("want error 'nil'; got '%s'", gotError)
						return
					}
					if !strings.Contains(gotError, td.wantErr) {
						tt.Fatalf("want error '%s'; got '%s'", td.wantErr, gotError)
					}
				})

			_ = root.Execute()

			if td.wantErr == "" && gotCfg != td.wantCfg {
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
			t.Fatalf("want no error; got '%s'", gotError)

			if !strings.Contains(gotError, "unable to read") {
				t.Fatalf("want error 'unable to read'; got '%s'", gotError)
			}
		})
	_ = cmd.Execute()
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
		args = append(args, fmt.Sprintf("-t=%s", tmpl))
	}
	return args
}
