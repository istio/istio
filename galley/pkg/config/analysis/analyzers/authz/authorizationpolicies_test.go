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

package authz

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNamespaceMatch(t *testing.T) {
	assert := assert.New(t)

	assert.True(namespaceMatch("test-login", "*"))

	assert.True(namespaceMatch("test-login", "test-*"))
	assert.False(namespaceMatch("test-login", "*-test"))

	assert.False(namespaceMatch("test-login", "login-*"))
	assert.True(namespaceMatch("test-login", "*-login"))
}

func TestHostMatch(t *testing.T) {
	assert := assert.New(t)

	assert.True(hostMatch("*.istio.io", "*"))
	assert.True(hostMatch("subsystem.istio.io", "*"))
	assert.True(hostMatch("reviews", "*"))
	assert.True(hostMatch("reviews.bookinfo.svc.cluster.local", "*"))

	assert.True(hostMatch("*.istio.io", "*.istio.io"))
	assert.True(hostMatch("subsystem.istio.io", "*.istio.io"))
	assert.False(hostMatch("reviews", "*.istio.io"))
	assert.False(hostMatch("reviews.bookinfo.svc.cluster.local", "*.istio.io"))

	assert.True(hostMatch("*.istio.io", "subsystem.istio.io"))
	assert.True(hostMatch("subsystem.istio.io", "subsystem.istio.io"))
	assert.False(hostMatch("reviews", "subsystem.istio.io"))
	assert.False(hostMatch("reviews.bookinfo.svc.cluster.local", "subsystem.istio.io"))

	assert.True(hostMatch("*.istio.io", "*stio.io"))
	assert.True(hostMatch("subsystem.istio.io", "*stio.io"))
	assert.False(hostMatch("reviews", "*stio.io"))
	assert.False(hostMatch("reviews.bookinfo.svc.cluster.local", "*stio.io"))

	assert.False(hostMatch("*.istio.io", "subsystem.istio*"))
	assert.True(hostMatch("subsystem.istio.io", "subsystem.istio*"))
	assert.False(hostMatch("reviews", "subsystem.istio*"))
	assert.False(hostMatch("reviews.bookinfo.svc.cluster.local", "subsystem.istio*"))
}
