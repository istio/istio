//go:build gofuzz
// +build gofuzz

// Copyright Istio Authors
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

package gateway

import (
	"fmt"
	fuzz "github.com/AdaLogics/go-fuzz-headers"
)

func validateKubernetesResouces(r *KubernetesResources) error {
	for _, gwc := range r.GatewayClass {
		if gwc.Spec == nil {
			return fmt.Errorf("Resource is nil")
		}
	}
	for _, rp := range r.ReferencePolicy {
		if rp.Spec == nil {
			return fmt.Errorf("Resource is nil")
		}
	}
	for _, hr := range r.HTTPRoute {
		if hr.Spec == nil {
			return fmt.Errorf("Resource is nil")
		}
	}
	for _, tr := range r.TLSRoute {
		if tr.Spec == nil {
			return fmt.Errorf("Resource is nil")
		}
	}
	for _, g := range r.Gateway {
		if g.Spec == nil {
			return fmt.Errorf("Resource is nil")
		}
	}
	for _, tr := range r.TCPRoute {
		if tr.Spec == nil {
			return fmt.Errorf("Resource is nil")
		}
	}
	return nil
}

func ConvertResourcesFuzz(data []byte) int {
	r := &KubernetesResources{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(r)
	if err != nil {
		return 0
	}
	err = validateKubernetesResouces(r)
	if err != nil {
		return 0
	}
	_ = convertResources(r)
	return 1
}
