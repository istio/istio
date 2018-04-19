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

package analyzer

import (
	"istio.io/istio/galley/pkg/api"
)

func CheckServiceConfig(config *api.ServiceConfig) *Messages {
	m := &Messages{}

	checkEmpty(m, "service name", config.Name)

	for _, instance := range config.Instances {
		checkInstance(m, instance)
	}

	return m
}

func checkInstance(m *Messages, instance *api.InstanceDecl) {
	checkEmpty(m, "instance name", instance.Name)
	checkEmpty(m, "instance template", instance.Template)
}
