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

package flag

import (
	"testing"

	"github.com/spf13/pflag"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestBind(t *testing.T) {
	type Options struct {
		A, B, C string
		Env     string
		Bool    bool
	}
	opts := Options{
		B: "b-def",
	}
	test.SetEnvForTest(t, "TEST_ENV", "from-env")
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	Bind(fs, "a", "", "desc", &opts.A)
	Bind(fs, "bool", "", "desc", &opts.Bool)
	BindEnv(fs, "test-env", "", "desc", &opts.Env)
	fs.Set("a", "a-set")
	fs.Set("bool", "true")
	assert.Equal(t, opts, Options{
		A:    "a-set",
		B:    "b-def",
		Env:  "from-env",
		Bool: true,
	})
}
