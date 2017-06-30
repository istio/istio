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
	"github.com/golang/protobuf/proto"

	pb "istio.io/api/mixer/v1/config/descriptor"
	adptConfig "istio.io/mixer/pkg/adapter/config"
	sample_report "istio.io/mixer/template/sample/report"
)

var (
	templateInfos = map[string]Info{
		sample_report.TemplateName: {
			InferTypeFn:     inferTypeForSampleReport,
			CnstrDefConfig:  &sample_report.ConstructorParam{},
			ConfigureTypeFn: configureTypeForSampleReport,
		},
	}
)

func inferTypeForSampleReport(cp proto.Message, tEvalFn TypeEvalFn) (proto.Message, error) {
	var err error

	// Mixer framework should have ensured the type safety.
	cpb := cp.(*sample_report.ConstructorParam)

	infrdType := &sample_report.Type{}
	if infrdType.Value, err = tEvalFn(cpb.Value); err != nil {
		return nil, err
	}

	infrdType.Dimensions = make(map[string]pb.ValueType)
	for k, v := range cpb.Dimensions {
		if infrdType.Dimensions[k], err = tEvalFn(v); err != nil {
			return nil, err
		}
	}

	return infrdType, nil
}

func configureTypeForSampleReport(types map[string]proto.Message, builder *adptConfig.HandlerBuilder) error {

	// Mixer framework should have ensured the type safety.
	castedBuilder := (*builder).(sample_report.SampleProcessorBuilder)

	castedTypes := make(map[string]*sample_report.Type)
	for k, v := range types {
		// Mixer framework should have ensured the type safety.
		v1 := v.(*sample_report.Type)
		castedTypes[k] = v1
	}

	return castedBuilder.ConfigureSample(castedTypes)
}
