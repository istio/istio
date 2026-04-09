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

package v1alpha3

import (
	"testing"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/test"
)

// BenchmarkResolveHost benchmarks single host resolution
func BenchmarkResolveHost(b *testing.B) {
	test.SetForTest(&testing.T{}, &features.EnableShortNameResolution, true)

	resolver := NewShortNameResolver(nil)
	hostname := "myservice"
	namespace := "default"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver.ResolveHost(hostname, namespace)
	}
}

// BenchmarkResolveBatch benchmarks batch resolution
func BenchmarkResolveBatch(b *testing.B) {
	test.SetForTest(&testing.T{}, &features.EnableShortNameResolution, true)

	resolver := NewShortNameResolver(nil)
	hostnames := []string{
		"service1",
		"service2",
		"service3",
		"service4",
		"service5",
	}
	namespace := "default"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver.ResolveBatch(hostnames, namespace)
	}
}

// BenchmarkResolveHostWithTargetNamespace benchmarks resolution with target namespace
func BenchmarkResolveHostWithTargetNamespace(b *testing.B) {
	test.SetForTest(&testing.T{}, &features.EnableShortNameResolution, true)

	resolver := NewShortNameResolver(nil)
	hostname := "myservice"
	sourceNamespace := "app-ns"
	targetNamespace := "other-ns"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver.ResolveHostWithTargetNamespace(hostname, sourceNamespace, targetNamespace)
	}
}

// BenchmarkResolveFQDN benchmarks resolution of already-FQDN names (should be fast)
func BenchmarkResolveFQDN(b *testing.B) {
	test.SetForTest(&testing.T{}, &features.EnableShortNameResolution, true)

	resolver := NewShortNameResolver(nil)
	hostname := "myservice.default.svc.cluster.local"
	namespace := "default"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver.ResolveHost(hostname, namespace)
	}
}

// BenchmarkResolveDisabled benchmarks resolution when feature is disabled
func BenchmarkResolveDisabled(b *testing.B) {
	test.SetForTest(&testing.T{}, &features.EnableShortNameResolution, false)

	resolver := NewShortNameResolver(nil)
	hostname := "myservice"
	namespace := "default"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver.ResolveHost(hostname, namespace)
	}
}
