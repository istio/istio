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

package cli

import (
	"testing"

	"github.com/spf13/pflag"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/test/util/assert"
)

func Test_AddRootFlags(t *testing.T) {
	flags := &pflag.FlagSet{}
	r := AddRootFlags(flags)
	impersonateConfig := rest.ImpersonationConfig{}
	assert.Equal(t, *r.impersonate, impersonateConfig.UserName)
	assert.Equal(t, *r.impersonateUID, impersonateConfig.UID)
	assert.Equal(t, *r.impersonateGroup, impersonateConfig.Groups)
	assert.Equal(t, *r.kubeTimeout, "15s")
}
