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

// Service defines OSB service data structure.
type Service struct {
	Name           string   `json:"name"`
	ID             string   `json:"id"`
	Description    string   `json:"description"`
	Bindable       bool     `json:"bindable"`
	PlanUpdateable bool     `json:"plan_updateable, omitempty"`
	Tags           []string `json:"tags, omitempty"`
	Requires       []string `json:"requires, omitempty"`

	Metadata        interface{}   `json:"metadata, omitempty"`
	Plans           []ServicePlan `json:"plans"`
	DashboardClient interface{}   `json:"dashboard_client"`
}

// AddPlan adds a service plan into the service's plans.
func (s *Service) AddPlan(plan *ServicePlan) {
	s.Plans = append(s.Plans, *plan)
}

// NewService creates a service from service class config proto.
func NewService(sp *brokerconfig.ServiceClass) *Service {
	s := new(Service)
	cs := sp.GetEntry()
	s.Name = cs.GetName()
	s.ID = cs.GetId()
	s.Description = cs.GetDescription()
	return s
}
