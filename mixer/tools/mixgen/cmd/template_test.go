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

package cmd

import (
	"fmt"
	"strings"
	"testing"
)

type templateCmdTestdata struct {
	templateFile string
	wantCfg      string
	wantErr      string
}

func TestTemplateCmd(t *testing.T) {
	for i, td := range []templateCmdTestdata{
		{
			templateFile: "testdata/simple/foo.descriptor",
			// nolint:lll
			wantCfg: `# this config is created through command
# mixgen template -d testdata/simple/foo.descriptor -n myTemplateResourceName --namespace mynamespace
apiVersion: "config.istio.io/v1alpha2"
kind: template
metadata:
  name: myTemplateResourceName
  namespace: mynamespace
spec:
  descriptor: "CvMBCh5nb29nbGUvcHJvdG9idWYvZHVyYXRpb24ucHJvdG8SD2dvb2dsZS5wcm90b2J1ZiI6CghEdXJhdGlvbhIYCgdzZWNvbmRzGAEgASgDUgdzZWNvbmRzEhQKBW5hbm9zGAIgASgFUgVuYW5vc0J8ChNjb20uZ29vZ2xlLnByb3RvYnVmQg1EdXJhdGlvblByb3RvUAFaKmdpdGh1Yi5jb20vZ29sYW5nL3Byb3RvYnVmL3B0eXBlcy9kdXJhdGlvbvgBAaICA0dQQqoCHkdvb2dsZS5Qcm90b2J1Zi5XZWxsS25vd25UeXBlc2IGcHJvdG8zCvcBCh9nb29nbGUvcHJvdG9idWYvdGltZXN0YW1wLnByb3RvEg9nb29nbGUucHJvdG9idWYiOwoJVGltZXN0YW1wEhgKB3NlY29uZHMYASABKANSB3NlY29uZHMSFAoFbmFub3MYAiABKAVSBW5hbm9zQn4KE2NvbS5nb29nbGUucHJvdG9idWZCDlRpbWVzdGFtcFByb3RvUAFaK2dpdGh1Yi5jb20vZ29sYW5nL3Byb3RvYnVmL3B0eXBlcy90aW1lc3RhbXD4AQGiAgNHUEKqAh5Hb29nbGUuUHJvdG9idWYuV2VsbEtub3duVHlwZXNiBnByb3RvMwrQCQoZcG9saWN5L3YxYmV0YTEvdHlwZS5wcm90bxIUaXN0aW8ucG9saWN5LnYxYmV0YTEaHmdvb2dsZS9wcm90b2J1Zi9kdXJhdGlvbi5wcm90bxofZ29vZ2xlL3Byb3RvYnVmL3RpbWVzdGFtcC5wcm90byKkBQoFVmFsdWUSIwoMc3RyaW5nX3ZhbHVlGAEgASgJSABSC3N0cmluZ1ZhbHVlEiEKC2ludDY0X3ZhbHVlGAIgASgDSABSCmludDY0VmFsdWUSIwoMZG91YmxlX3ZhbHVlGAMgASgBSABSC2RvdWJsZVZhbHVlEh8KCmJvb2xfdmFsdWUYBCABKAhIAFIJYm9vbFZhbHVlEksKEGlwX2FkZHJlc3NfdmFsdWUYBSABKAsyHy5pc3Rpby5wb2xpY3kudjFiZXRhMS5JUEFkZHJlc3NIAFIOaXBBZGRyZXNzVmFsdWUSSgoPdGltZXN0YW1wX3ZhbHVlGAYgASgLMh8uaXN0aW8ucG9saWN5LnYxYmV0YTEuVGltZVN0YW1wSABSDnRpbWVzdGFtcFZhbHVlEkcKDmR1cmF0aW9uX3ZhbHVlGAcgASgLMh4uaXN0aW8ucG9saWN5LnYxYmV0YTEuRHVyYXRpb25IAFINZHVyYXRpb25WYWx1ZRJUChNlbWFpbF9hZGRyZXNzX3ZhbHVlGAggASgLMiIuaXN0aW8ucG9saWN5LnYxYmV0YTEuRW1haWxBZGRyZXNzSABSEWVtYWlsQWRkcmVzc1ZhbHVlEkUKDmRuc19uYW1lX3ZhbHVlGAkgASgLMh0uaXN0aW8ucG9saWN5LnYxYmV0YTEuRE5TTmFtZUgAUgxkbnNOYW1lVmFsdWUSOAoJdXJpX3ZhbHVlGAogASgLMhkuaXN0aW8ucG9saWN5LnYxYmV0YTEuVXJpSABSCHVyaVZhbHVlEksKEHN0cmluZ19tYXBfdmFsdWUYCyABKAsyHy5pc3Rpby5wb2xpY3kudjFiZXRhMS5TdHJpbmdNYXBIAFIOc3RyaW5nTWFwVmFsdWVCBwoFdmFsdWUiIQoJSVBBZGRyZXNzEhQKBXZhbHVlGAEgASgMUgV2YWx1ZSI7CghEdXJhdGlvbhIvCgV2YWx1ZRgBIAEoCzIZLmdvb2dsZS5wcm90b2J1Zi5EdXJhdGlvblIFdmFsdWUiPQoJVGltZVN0YW1wEjAKBXZhbHVlGAEgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcFIFdmFsdWUiHwoHRE5TTmFtZRIUCgV2YWx1ZRgBIAEoCVIFdmFsdWUihwEKCVN0cmluZ01hcBJACgV2YWx1ZRgBIAMoCzIqLmlzdGlvLnBvbGljeS52MWJldGExLlN0cmluZ01hcC5WYWx1ZUVudHJ5UgV2YWx1ZRo4CgpWYWx1ZUVudHJ5EhAKA2tleRgBIAEoCVIDa2V5EhQKBXZhbHVlGAIgASgJUgV2YWx1ZToCOAEiJAoMRW1haWxBZGRyZXNzEhQKBXZhbHVlGAEgASgJUgV2YWx1ZSIbCgNVcmkSFAoFdmFsdWUYASABKAlSBXZhbHVlQiBaG2lzdGlvLmlvL2FwaS9wb2xpY3kvdjFiZXRhMbgBAWIGcHJvdG8zCo0BChl0ZXN0ZGF0YS9zaW1wbGUvZm9vLnByb3RvEhlpc3Rpby5taXhlci5hZGFwdGVyLnF1b3RhGhlwb2xpY3kvdjFiZXRhMS90eXBlLnByb3RvIjIKAWESLQoDdmFsGAEgASgLMhsuaXN0aW8ucG9saWN5LnYxYmV0YTEuVmFsdWVSA3ZhbGIGcHJvdG8z"
---
`,
		},
		{
			templateFile: "testdata/simple/foo_without_imports.descriptor",
			wantErr: "template in invalid: the file descriptor set was created without including imports. Please run " +
				"protoc with `--include_imports` flag",
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(tt *testing.T) {
			args := []string{"template", "-d", td.templateFile, "-n", "myTemplateResourceName", "--namespace", "mynamespace"}

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
					if !strings.Contains(fmt.Sprintf(format, a...), td.wantErr) {
						tt.Fatalf("want error '%s'; got '%s'", td.wantErr, fmt.Sprintf(format, a...))
					}
				})

			_ = root.Execute()

			if td.wantErr == "" && gotCfg != td.wantCfg {
				tt.Errorf("want :\n%v\ngot :\n%v", td.wantCfg, gotCfg)
			}
		})
	}
}

func TestTemplateCmd_NoInputFile(t *testing.T) {
	var gotError string
	cmd := GetRootCmd([]string{"template"},
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
