// Copyright 2018 Istio Authors
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

package util

import (
	"fmt"
)

// K8sSaClaims implements the k8s service account claims.
type K8sSaClaims struct {
	Issuer             string `json:"iss,omitempty"`
	Namespace          string `json:"kubernetes.io/serviceaccount/namespace,omitempty"`
	SecretName         string `json:"kubernetes.io/serviceaccount/secret.name,omitempty"`
	ServiceAccountName string `json:"kubernetes.io/serviceaccount/service-account.name,omitempty"`
	ServiceAccountUID  string `json:"kubernetes.io/serviceaccount/service-account.uid,omitempty"`
	Subject            string `json:"sub,omitempty"`
}

// Valid returns an error if the k8s claims are invalid
func (c K8sSaClaims) Valid() error {
	if len(c.Namespace) == 0 {
		return fmt.Errorf("empty namespace in the k8s service account token")
	}
	if len(c.ServiceAccountName) == 0 {
		return fmt.Errorf("empty service account name in the k8s service account token")
	}
	return nil
}
