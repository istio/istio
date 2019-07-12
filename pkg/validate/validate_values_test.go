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

package validate

import (
	"fmt"
	"testing"

	"github.com/ghodss/yaml"

	"istio.io/operator/pkg/util"
)

func TestValidateValues(t *testing.T) {
	tests := []struct {
		desc     string
		yamlStr  string
		wantErrs util.Errors
	}{
		{
			desc: "nil success",
		},
		{
			desc: "ProxyConfig",
			yamlStr: `
global:
  proxy:
    enabled: true
    includeIpRanges: "1.1.0.0/16,2.2.0.0/16"
    excludeIpRanges: "3.3.0.0/16,4.4.0.0/16"
    includeInboundPorts: "111,222"
    excludeInboundPorts: "333,444"
    clusterDomain: "my.domain"
    podDnsSearchNamespaces: "my-namespace"
    interceptionMode: TPROXY
    connectTimeout: "11s"
    drainDuration: "22s"
    parentShutdownDuration : "33s"
    concurrency: 5
`,
		},
		{
			desc: "BadIPRange",
			yamlStr: `
global:
  proxy:
    includeIpRanges: "1.1.0.256/16,2.2.0.257/16"
    excludeIpRanges: "3.3.0.0/33,4.4.0.0/34"
`,
			wantErrs: makeErrors([]string{`global.proxy.excludeIpRanges invalid CIDR address: 3.3.0.0/33`,
				`global.proxy.excludeIpRanges invalid CIDR address: 4.4.0.0/34`,
				`global.proxy.includeIpRanges invalid CIDR address: 1.1.0.256/16`,
				`global.proxy.includeIpRanges invalid CIDR address: 2.2.0.257/16`}),
		},
		{
			desc: "BadIPMalformed",
			yamlStr: `
global:
  proxy:
    includeIpRanges: "1.2.3/16,1.2.3.x/16"
`,
			wantErrs: makeErrors([]string{`global.proxy.includeIpRanges invalid CIDR address: 1.2.3/16`,
				`global.proxy.includeIpRanges invalid CIDR address: 1.2.3.x/16`}),
		},
		{
			desc: "BadPortRange",
			yamlStr: `
global:
  proxy:
    includeInboundPorts: "111,65536"
    excludeInboundPorts: "-1,444"
`,
			wantErrs: makeErrors([]string{`value global.proxy.excludeInboundPorts:-1 falls outside range [0, 65535]`,
				`value global.proxy.includeInboundPorts:65536 falls outside range [0, 65535]`}),
		},
		{
			desc: "BadPortMalformed",
			yamlStr: `
global:
  proxy:
    includeInboundPorts: "111,222x"
`,
			wantErrs: makeErrors([]string{`global.proxy.includeInboundPorts : strconv.ParseInt: parsing "222x": invalid syntax`}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			root := make(map[string]interface{})
			err := yaml.Unmarshal([]byte(tt.yamlStr), &root)
			if err != nil {
				t.Fatalf("yaml.Unmarshal(%s): got error %s", tt.desc, err)
			}
			errs := CheckValues(root)
			if gotErr, wantErr := errs, tt.wantErrs; !util.EqualErrors(gotErr, wantErr) {
				t.Errorf("CheckValues(%s)(%v): gotErr:%s, wantErr:%s", tt.desc, tt.yamlStr, gotErr, wantErr)
			}
		})
	}
}

func makeErrors(estr []string) util.Errors {
	var errs util.Errors
	for _, s := range estr {
		errs = util.AppendErr(errs, fmt.Errorf("%s", s))
	}
	return errs
}
