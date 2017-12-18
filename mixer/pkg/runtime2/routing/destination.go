// Copyright 2017 Istio Authors
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

package routing

//import (
//	"istio.io/istio/mixer/pkg/adapter"
//	"istio.io/istio/mixer/pkg/attribute"
//	"istio.io/istio/mixer/pkg/il/compiled"
//	"istio.io/istio/mixer/pkg/template"
//)

//
//func (d *HandlerEntry) BuildInstances(bag attribute.Bag) []interface{} {
//	// TODO: we should avoid this allocation, if possible.
//	result := make([]interface{}, 0, d.MaxInstances())
//
//	for _, i := range d.Inputs {
//		if i.Condition != nil {
//			match, err := i.Condition.EvaluateBoolean(bag)
//			if err != nil {
//				// TODO: log
//				continue
//			}
//
//			if !match {
//				continue
//			}
//		}
//
//		for _, b := range i.Builders {
//			instance, err := b(bag)
//			if err != nil {
//				// TODO: log
//				continue
//			}
//			result = append(result, instance)
//		}
//	}
//
//	return result
//}
