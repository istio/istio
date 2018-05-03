//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package analyze

import (
	"istio.io/istio/galley/pkg/analyze/check"
	"istio.io/istio/galley/pkg/analyze/message"
	"istio.io/istio/galley/pkg/api/service/dev"
)

// ProducerService analyzes a ProducerService and returns the results.
func ProducerService(cfg *dev.ProducerService) message.List {
	var m message.List

	checkService(&m, cfg.Service)

	for _, ins := range cfg.Instances {
		checkInstance(&m, ins)
	}
	return m
}

func checkService(m *message.List, selector *dev.ServiceSelector) {
	check.Nil(m, "service", selector)
	if selector == nil {
		return
	}

	check.Empty(m, "service name", selector.Name)
}

func checkInstance(m *message.List, instance *dev.InstanceDecl) {
	check.Empty(m, "instance name", instance.Name)
	check.Empty(m, "instance template", instance.Template)
}
