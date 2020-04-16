// Copyright 2020 Istio Authors
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

	"k8s.io/helm/pkg/proto/hapi/chart"
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

// TODO(adiprerepa): find a way to pass a chart in and test it.
func TestRenderManifest(t *testing.T) {
	tests := []struct {
		desc                  string
		inValues              string
		inChart               chart.Chart
		objFileTemplateReader FileTemplateRenderer
		wantResult            string
		wantErr               error
	}{
		{
			desc:     "empty",
			inValues: "",
			inChart: chart.Chart{
				Metadata:             nil,
				Templates:            nil,
				Dependencies:         nil,
				Values:               nil,
				Files:                nil,
				XXX_NoUnkeyedLiteral: struct{}{},
				XXX_unrecognized:     nil,
				XXX_sizecache:        0,
			},
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
			desc:     "not-started",
			inValues: "foo-bar",
			inChart: chart.Chart{
				Metadata:             nil,
				Templates:            nil,
				Dependencies:         nil,
				Values:               nil,
				Files:                nil,
				XXX_NoUnkeyedLiteral: struct{}{},
				XXX_unrecognized:     nil,
				XXX_sizecache:        0,
			},
			objFileTemplateReader: FileTemplateRenderer{
				namespace:        "name-space",
				componentName:    "foo-component",
				helmChartDirPath: "/foo/bar",
				chart:            nil,
				started:          false,
			},
			wantResult: "",
			wantErr:    errors.New("fileTemplateRenderer for foo-component not started in renderChart"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if res, err := tt.objFileTemplateReader.RenderManifest(tt.inValues); !(tt.wantResult == res) || tt.wantErr.Error() != err.Error() {
				t.Errorf("%s: expected %s, got %s", tt.desc, tt.wantResult, res)
			}
		})
	}
}
