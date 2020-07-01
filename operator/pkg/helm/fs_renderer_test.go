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

package helm

import (
	"errors"
	"testing"

	"helm.sh/helm/v3/pkg/chart"
)

func TestNewFileTemplateRenderer(t *testing.T) {
	tests := []struct {
		desc               string
		inHelmChartDirPath string
		inComponentName    string
		inNamespace        string
		want               FileTemplateRenderer
	}{
		{
			desc:               "empty",
			inHelmChartDirPath: "",
			inComponentName:    "",
			inNamespace:        "",
			want: FileTemplateRenderer{
				namespace:        "",
				componentName:    "",
				helmChartDirPath: "",
				chart:            nil,
				started:          false,
			},
		},
		{
			desc:               "initialized-notrunning",
			inHelmChartDirPath: "/goo/bar/goo",
			inComponentName:    "bazzycomponent",
			inNamespace:        "fooeynamespace",
			want: FileTemplateRenderer{
				namespace:        "fooeynamespace",
				componentName:    "bazzycomponent",
				helmChartDirPath: "/goo/bar/goo",
				chart:            nil,
				started:          false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if fileTemplateRenderer := NewFileTemplateRenderer(tt.inHelmChartDirPath, tt.inComponentName, tt.inNamespace); *fileTemplateRenderer != tt.want {
				t.Errorf("%s, want: %+v, got: %+v", tt.desc, tt.want, *fileTemplateRenderer)
			}
		})
	}
}

func TestRenderManifest(t *testing.T) {
	tests := []struct {
		desc                  string
		inValues              string
		inChart               chart.Chart
		startRender           bool
		inPath                string
		objFileTemplateReader FileTemplateRenderer
		wantResult            string
		wantErr               error
	}{
		{
			desc:        "not-started",
			inValues:    "",
			startRender: false,
			inChart:     chart.Chart{},
			objFileTemplateReader: FileTemplateRenderer{
				namespace:        "",
				componentName:    "",
				helmChartDirPath: "",
				chart:            nil,
				started:          false,
			},
			wantResult: "",
			wantErr:    errors.New("fileTemplateRenderer for  not started in renderChart"),
		},
		{
			desc: "started-random-template",
			inValues: `
description: test
`,
			inPath:      "testdata/render/Chart.yaml",
			startRender: true,
			objFileTemplateReader: FileTemplateRenderer{
				namespace:        "name-space",
				componentName:    "foo-component",
				helmChartDirPath: "testdata/render",
			},
			wantResult: `apiVersion: v1
description: 
name: addon
version: 1.1.0
appVersion: 1.1.0
tillerVersion: ">=2.7.2"
keywords:
  - istio-addon

---
`,
			wantErr: nil,
		},
		{
			desc:        "bad-file-path",
			inValues:    "",
			inPath:      "foo/bar/Chart.yaml",
			startRender: true,
			objFileTemplateReader: FileTemplateRenderer{
				namespace:        "name-space",
				componentName:    "foo-component",
				helmChartDirPath: "foo/bar",
			},
			wantResult: "",
			wantErr:    errors.New("stat foo/bar: no such file or directory"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if tt.startRender {
				err := tt.objFileTemplateReader.Run()
				if err != nil && tt.wantErr != nil {
					if err.Error() != tt.wantErr.Error() {
						t.Errorf("%s: expected err :%v got %v", tt.desc, tt.wantErr.Error(), err.Error())
					}
				}
			}
			if res, err := tt.objFileTemplateReader.RenderManifest(tt.inValues); res != tt.wantResult ||
				((tt.wantErr != nil && err == nil) || (tt.wantErr == nil && err != nil)) {
				t.Errorf("%s: \nexpected vals: \n%v\n\nexpected err:%v\ngot vals:\n%v\n\n got err %v",
					tt.desc, tt.wantResult, tt.wantErr, res, err)
			}
		})
	}
}
