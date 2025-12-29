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

package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"istio.io/istio/istioctl/pkg/util/testutil"
	"istio.io/istio/pkg/config/constants"
)

func TestConfigList(t *testing.T) {
	cases := []testutil.TestCase{
		//{ // case 0
		//	Args:           strings.Split("get istioNamespace", " "),
		//	ExpectedRegexp: regexp.MustCompile("Configure istioctl defaults"),
		//	WantException:  false,
		//},
		{ // case 1
			Args: strings.Split("list", " "),
			ExpectedOutput: `FLAG                    VALUE            FROM
authority                                default
cert-dir                                 default
insecure                                 default
istioNamespace          istio-system     default
plaintext                                default
prefer-experimental                      default
xds-address                              default
xds-port                15012            default
`,
			WantException: false,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.Args, " ")), func(t *testing.T) {
			testutil.VerifyOutput(t, Cmd(), c)
		})
	}
}

func init() {
	viper.SetDefault("istioNamespace", constants.IstioSystemNamespace)
	viper.SetDefault("xds-port", 15012)
}
