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

package dependencies

import (
	"testing"

	// Create a new network namespace. This will have the 'lo' interface ready but nothing else.
	_ "github.com/howardjohn/unshare-go/netns"
	// Create a new user namespace. This will map the current UID to 0.
	_ "github.com/howardjohn/unshare-go/userns"
	"github.com/vishvananda/netlink"

	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/util/sets"
)

func TestRunInSandbox(t *testing.T) {
	original := file.AsStringOrFail(t, "/etc/nsswitch.conf")
	var sandboxed string

	originalInterfaces := getInterfaces(t)
	// We should be in a minimal sandbox with only 'lo'
	assert.Equal(t, originalInterfaces, sets.New("lo"))
	var interfaces sets.String

	// Due to unshare-go imports above, this can run
	assert.NoError(t, runInSandbox("", func() error {
		// We should have overwritten this file with /dev/null
		sandboxed = file.AsStringOrFail(t, "/etc/nsswitch.conf")
		// We should still be in the same network namespace, and hence have the same interfaces
		interfaces = getInterfaces(t)
		return nil
	}))
	after := file.AsStringOrFail(t, "/etc/nsswitch.conf")
	assert.Equal(t, sandboxed, "")
	assert.Equal(t, original, after)
	assert.Equal(t, interfaces, originalInterfaces)
}

func getInterfaces(t *testing.T) sets.String {
	l, err := netlink.LinkList()
	assert.NoError(t, err)
	links := sets.New(slices.Map(l, func(e netlink.Link) string {
		return e.Attrs().Name
	})...)
	return links
}
