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

package multicluster

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestCompareServicePorts(t *testing.T) {
	tests := []struct {
		name     string
		a        []corev1.ServicePort
		b        []corev1.ServicePort
		expected bool
	}{
		{
			name:     "both empty",
			a:        []corev1.ServicePort{},
			b:        []corev1.ServicePort{},
			expected: true,
		},
		{
			name: "identical single port",
			a: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			},
			b: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			},
			expected: true,
		},
		{
			name: "identical multiple ports",
			a: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
				{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP},
			},
			b: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
				{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP},
			},
			expected: true,
		},
		{
			name: "same ports different order",
			a: []corev1.ServicePort{
				{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP},
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			},
			b: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
				{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP},
			},
			expected: true,
		},
		{
			name: "different port numbers",
			a: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			},
			b: []corev1.ServicePort{
				{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP},
			},
			expected: false,
		},
		{
			name: "different port names",
			a: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			},
			b: []corev1.ServicePort{
				{Name: "web", Port: 80, Protocol: corev1.ProtocolTCP},
			},
			expected: false,
		},
		{
			name: "different protocols",
			a: []corev1.ServicePort{
				{Name: "dns", Port: 53, Protocol: corev1.ProtocolTCP},
			},
			b: []corev1.ServicePort{
				{Name: "dns", Port: 53, Protocol: corev1.ProtocolUDP},
			},
			expected: false,
		},
		{
			name: "different number of ports",
			a: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			},
			b: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
				{Name: "https", Port: 443, Protocol: corev1.ProtocolTCP},
			},
			expected: false,
		},
		{
			name: "one empty",
			a: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			},
			b:        []corev1.ServicePort{},
			expected: false,
		},
		{
			name: "with target port",
			a: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
			},
			b: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
			},
			expected: true,
		},
		{
			name: "different target port",
			a: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
			},
			b: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(9090), Protocol: corev1.ProtocolTCP},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make copies to avoid modifying the original test data
			aCopy := make([]corev1.ServicePort, len(tt.a))
			bCopy := make([]corev1.ServicePort, len(tt.b))
			copy(aCopy, tt.a)
			copy(bCopy, tt.b)

			result := compareServicePorts(aCopy, bCopy)
			if result != tt.expected {
				t.Errorf("compareServicePorts() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestCompareServicePortsNilSlices(t *testing.T) {
	// Test with nil slices
	if !compareServicePorts(nil, nil) {
		t.Error("compareServicePorts(nil, nil) should return true")
	}

	ports := []corev1.ServicePort{{Name: "http", Port: 80}}
	if compareServicePorts(nil, ports) {
		t.Error("compareServicePorts(nil, ports) should return false")
	}
	if compareServicePorts(ports, nil) {
		t.Error("compareServicePorts(ports, nil) should return false")
	}
}
