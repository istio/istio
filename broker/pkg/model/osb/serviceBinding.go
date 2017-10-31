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

// ServiceBinding defines OSB service binding data structure.
type ServiceBinding struct {
	ID                string `json:"id"`
	ServiceID         string `json:"service_id"`
	AppID             string `json:"app_id"`
	ServicePlanID     string `json:"service_plan_id"`
	PrivateKey        string `json:"private_key"`
	ServiceInstanceID string `json:"service_instance_id"`
}

// CreateServiceBindingResponse defines OSB service binding response data structure.
type CreateServiceBindingResponse struct {
	// SyslogDrainUrl string      `json:"syslog_drain_url, omitempty"`
	Credentials interface{} `json:"credentials"`
}

// Credential defines OSB credential data structure.
type Credential struct {
	PublicIP   string `json:"public_ip"`
	UserName   string `json:"username"`
	PrivateKey string `json:"private_key"`
}
