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

package osb

import brokerconfig "istio.io/api/broker/v1/config"

// ServicePlan defines the OSB service plan data structure.
type ServicePlan struct {
	Name        string      `json:"name"`
	ID          string      `json:"id"`
	Description string      `json:"description"`
	Metadata    interface{} `json:"metadata, omitempty"`
	Free        bool        `json:"free, omitempty"`
}

// NewServicePlan creates a service plan from service plan config proto.
func NewServicePlan(sp *brokerconfig.ServicePlan) *ServicePlan {
	cp := sp.GetPlan()
	s := new(ServicePlan)
	s.Name = cp.GetName()
	s.ID = cp.GetId()
	s.Description = cp.GetDescription()
	return s
}
