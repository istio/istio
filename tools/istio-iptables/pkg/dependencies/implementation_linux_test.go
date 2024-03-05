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
	"github.com/vishvananda/netns"

	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/file"
)

func TestRunInSandbox(t *testing.T) {
	original := file.AsStringOrFail(t, "/etc/nsswitch.conf")
	var sandboxed string

	originalNetNS, err := netns.Get()
	assert.NoError(t, err)
	var sandboxedNetNS netns.NsHandle

	// Due to unshare-go imports above, this can run
	assert.NoError(t, runInSandbox("", func() error {
		// We should have overwritten this file with /dev/null
		sandboxed = file.AsStringOrFail(t, "/etc/nsswitch.conf")
		sandboxedNetNS, err = netns.Get()
		assert.NoError(t, err)
		return nil
	}))
	after := file.AsStringOrFail(t, "/etc/nsswitch.conf")
	assert.Equal(t, sandboxed, "")
	assert.Equal(t, original, after)
	assert.Equal(t, originalNetNS.Equal(sandboxedNetNS), true)
}
