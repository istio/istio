// Copyright 2017 the Istio Authors.
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

package aspect

import (
	"fmt"

	multierror "github.com/hashicorp/go-multierror"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
)

func evalAll(expressions map[string]string, attrs attribute.Bag, eval expr.Evaluator) (map[string]interface{}, error) {
	result := &multierror.Error{}
	labels := make(map[string]interface{}, len(expressions))
	for label, texpr := range expressions {
		val, err := eval.Eval(texpr, attrs)
		if err != nil {
			result = multierror.Append(result, fmt.Errorf("failed to construct value for label '%s' with err: %v", label, err))
			continue
		}
		labels[label] = val
	}
	return labels, result.ErrorOrNil()
}

func validateLabels(ceField string, labels map[string]string, labelDescs map[string]dpb.ValueType, v expr.Validator, df expr.AttributeDescriptorFinder) (
	ce *adapter.ConfigErrors) {

	if len(labels) != len(labelDescs) {
		ce = ce.Appendf(ceField, "wrong dimensions: descriptor expects %d labels, found %d labels", len(labelDescs), len(labels))
	}
	for name, exp := range labels {
		if labelType, found := labelDescs[name]; !found {
			ce = ce.Appendf(ceField, "wrong dimensions: extra label named %s", name)
		} else if err := v.AssertType(exp, df, labelType); err != nil {
			ce = ce.Appendf(ceField, "error type checking label '%s': %v", name, err)
		}
	}
	return
}

func validateTemplateExpressions(ceField string, expressions map[string]string, v expr.Validator, df expr.AttributeDescriptorFinder) (
	ce *adapter.ConfigErrors) {

	// We can't do type assertions since we don't know what each template param needs to resolve to, but we can
	// make sure they're syntactically correct and we have the attributes they need available in the system.
	for name, exp := range expressions {
		if _, err := v.TypeCheck(exp, df); err != nil {
			ce = ce.Appendf(ceField, "failed to parse expression '%s' with err: %v", name, err)
		}
	}
	return
}
