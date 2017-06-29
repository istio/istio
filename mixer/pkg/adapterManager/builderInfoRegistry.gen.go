// Copyright 2017 Istio Authors.
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

package adapterManager

import (
	"fmt"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapter/config"
	sample_report "istio.io/mixer/pkg/template/sample/report"
)

func DoesBuilderSupportsTemplate(hndlrBuilder config.HandlerBuilder, t adapter.SupportedTemplates, handlerName string) (bool, string) {
	errMsg := fmt.Sprintf("HandlerBuilder in adapter %s does not implement interface %s. "+
		"Therefore, it cannot support template %v", handlerName, adapter.BuilderNames[t], t)
	switch t {
	case adapter.SampleProcessorTemplate:
		if _, ok := hndlrBuilder.(sample_report.SampleProcessorBuilder); ok {
			return true, ""
		}
		return false, errMsg
	default:
		// Should not reach here
		panic(fmt.Sprintf("Supported template %v is not one of the allowed supported templates %v",
			t, adapter.AllSupportedTemplates))
	}
}
