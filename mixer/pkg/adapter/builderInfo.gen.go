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

package adapter

import (
	sample_report "istio.io/mixer/template/sample/report"
)

// SupportedTemplates is a string enum that expressess the template that an Adapter is intending to support.
type SupportedTemplates string

const (
	// SampleProcessor is for a Report processing Adapter...
	SampleProcessorTemplate SupportedTemplates = sample_report.TemplateName
)

var AllSupportedTemplates = []SupportedTemplates{
	SampleProcessorTemplate,
}

var BuilderNames = map[SupportedTemplates]string{
	SampleProcessorTemplate: "istio.io/mixer/template/sample/report.SampleProcessorBuilder",
}
