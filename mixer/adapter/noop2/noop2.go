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

package noop2

import (
	"fmt"

	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"

	adapter "istio.io/mixer/pkg/adapter"
	adapter_cnfg "istio.io/mixer/pkg/adapter/config"
	sample_report "istio.io/mixer/pkg/template/sample/report"
)

type (
	noop2Handler struct{}
	noop2Builder struct{}
)

///////////////// Configuration time Methods ///////////////

func (noop2Builder) Build(cnfg proto.Message) (adapter_cnfg.Handler, error) {
	fmt.Println("Build in noop Adapter called with", cnfg)
	return noop2Handler{}, nil
}

// Per template configuration methods
func (noop2Builder) ConfigureSample(typeParams map[string]*sample_report.Type) error {
	fmt.Println("ConfigureSample in noop Adapter called with", typeParams)
	return nil
}

////////////////// Runtime Methods //////////////////////////

func (noop2Handler) ReportSample(instances []*sample_report.Instance) error {
	fmt.Println("ReportSample in noop Adapter called with", instances)
	return nil
}

// Close closes all the open resources.
func (noop2Handler) Close() error { return nil }

// GetBuilderInfo returns the BuilderInfo associated with this adapter implementation.
func GetBuilderInfo() adapter.BuilderInfo {
	return adapter.BuilderInfo{
		Name:                   "noop2",
		Description:            "An adapter that does nothing, just echos the calls made from mixer",
		SupportedTemplates:     []adapter.SupportedTemplates{adapter.SampleProcessorTemplate},
		CreateHandlerBuilderFn: func() adapter_cnfg.HandlerBuilder { return noop2Builder{} },
		DefaultConfig:          &types.Empty{},
		ValidateConfig: func(msg proto.Message) error {
			fmt.Println("ValidateConfig called with input", msg)
			return nil
		},
	}
}
