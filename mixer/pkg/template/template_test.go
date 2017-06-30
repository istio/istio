// Copyright 2016 Istio Authors
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

package template

import (
	"reflect"
	"testing"

	sample_report "istio.io/mixer/template/sample/report"
)

func TestGetTemplateInfo(t *testing.T) {
	for _, tst := range []struct {
		template string
		expected Info
		present  bool
	}{
		{sample_report.TemplateName, Info{&sample_report.ConstructorParam{}, inferTypeForSampleReport, configureTypeForSampleReport}, true},
		{"unknown template", Info{}, false},
	} {
		t.Run(tst.template, func(t *testing.T) {
			tdf := templateRepo{}
			k, rpresent := tdf.GetTemplateInfo(tst.template)
			if rpresent != tst.present ||
				!reflect.DeepEqual(k.CnstrDefConfig, tst.expected.CnstrDefConfig) ||
				!reflect.DeepEqual(reflect.TypeOf(k.InferTypeFn), reflect.TypeOf(tst.expected.InferTypeFn)) {
				t.Errorf("got GetConstructorDefaultConfig(%s) = {%v,%v,%v}, want {%v,%v,%v}", tst.template, k.CnstrDefConfig, reflect.TypeOf(k.InferTypeFn), rpresent,
					tst.expected.CnstrDefConfig, reflect.TypeOf(tst.expected.InferTypeFn), tst.present)
			}
		})
	}
}
