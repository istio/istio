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

func Test_AddRootFlags_Revision(t *testing.T) {
	flags := &pflag.FlagSet{}
	r := AddRootFlags(flags)

	f := flags.Lookup(FlagRevision)
	if f == nil {
		t.Fatalf("expected %q flag to be registered", FlagRevision)
	}
	// -r is claimed by per-subcommand --revision registrations, so the root flag is long-only.
	if f.Shorthand != "" {
		t.Fatalf("expected no shorthand for root %q flag, got %q", FlagRevision, f.Shorthand)
	}
	assert.Equal(t, r.Revision(), "")

	if err := flags.Set(FlagRevision, "canary"); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, r.Revision(), "canary")
}
