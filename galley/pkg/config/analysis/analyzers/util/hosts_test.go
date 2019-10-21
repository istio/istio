// Copyright 2019 Istio Authors
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
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/resource"
)

func TestGetResourceNameFromHost(t *testing.T) {
	g := NewGomegaWithT(t)

	// FQDN, same namespace
	g.Expect(GetResourceNameFromHost("default", "foo.default.svc.cluster.local")).To(Equal(resource.NewName("default", "foo")))
	// FQDN, cross namespace
	g.Expect(GetResourceNameFromHost("default", "foo.other.svc.cluster.local")).To(Equal(resource.NewName("other", "foo")))
	// short name
	g.Expect(GetResourceNameFromHost("default", "foo")).To(Equal(resource.NewName("default", "foo")))
	// bogus FQDN (gets treated like a short name)
	g.Expect(GetResourceNameFromHost("default", "foo.svc.cluster.local")).To(Equal(resource.NewName("default", "foo.svc.cluster.local")))
}

func TestGetScopedFqdnHostname(t *testing.T) {
	g := NewGomegaWithT(t)

	// FQDN, same namespace, local scope
	g.Expect(GetScopedFqdnHostname("default", "default", "foo.default.svc.cluster.local")).To(Equal(ScopedFqdn("default/foo.default.svc.cluster.local")))
	// FQDN, cross namespace, local scope
	g.Expect(GetScopedFqdnHostname("default", "other", "foo.default.svc.cluster.local")).To(Equal(ScopedFqdn("default/foo.default.svc.cluster.local")))
	// FQDN, same namespace, all namespaces scope
	g.Expect(GetScopedFqdnHostname("*", "default", "foo.default.svc.cluster.local")).To(Equal(ScopedFqdn("*/foo.default.svc.cluster.local")))
	// FQDN, cross namespace, all namespaces scope
	g.Expect(GetScopedFqdnHostname("*", "other", "foo.default.svc.cluster.local")).To(Equal(ScopedFqdn("*/foo.default.svc.cluster.local")))

	// short name, same namespace, local scope
	g.Expect(GetScopedFqdnHostname("default", "default", "foo")).To(Equal(ScopedFqdn("default/foo.default.svc.cluster.local")))
	// short name, same namespace, all namespaces scope
	g.Expect(GetScopedFqdnHostname("*", "default", "foo")).To(Equal(ScopedFqdn("*/foo.default.svc.cluster.local")))

	// wildcard, local scope
	g.Expect(GetScopedFqdnHostname("foo", "foo", "*")).To(Equal(ScopedFqdn("foo/*")))
	// wildcard sub domain, local scope
	g.Expect(GetScopedFqdnHostname("foo", "foo", "*.xyz.abc")).To(Equal(ScopedFqdn("foo/*.xyz.abc")))
	// wildcard, all namespaces scope
	g.Expect(GetScopedFqdnHostname("*", "foo", "*")).To(Equal(ScopedFqdn("*/*")))
	// wildcard sub domain, all namespaces scope
	g.Expect(GetScopedFqdnHostname("*", "foo", "*.xyz.abc")).To(Equal(ScopedFqdn("*/*.xyz.abc")))

	// external host, local scope
	g.Expect(GetScopedFqdnHostname("foo", "foo", "xyz.abc")).To(Equal(ScopedFqdn("foo/xyz.abc")))
	// external host, all namespaces scope
	g.Expect(GetScopedFqdnHostname("*", "foo", "xyz.abc")).To(Equal(ScopedFqdn("*/xyz.abc")))
}
