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

package deployment

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/config"
	"istio.io/istio/pkg/test/util/file"
)

var additionalConfigs = &configs{}

func init() {
	flag.Var(additionalConfigs, "istio.test.echo.configs", "The path to a file containing a list of "+
		"echo.Config in YAML which will be added to the set of echos for every suite.")
}

// configs wraps a slice of echo.Config to implement the config.Value interface, allowing
// it to be configured by a flag, or within the test framework config file.
type configs []echo.Config

var _ config.Value = &configs{}

func (c *configs) String() string {
	buf := &bytes.Buffer{}
	for _, cc := range *c {
		_, _ = fmt.Fprintf(buf, "FQDN:     %s\n", cc.ClusterLocalFQDN())
		_, _ = fmt.Fprintf(buf, "Headless: %v\n", cc.Headless)
		_, _ = fmt.Fprintf(buf, "VM:       %v\n", cc.DeployAsVM)
		if cc.DeployAsVM {
			_, _ = fmt.Fprintf(buf, "VMDistro: %s\n", cc.VMDistro)
		}
		if cc.Cluster.Name() != "" {
			_, _ = fmt.Fprintf(buf, "Cluster:  %s\n", cc.Cluster.Name())
		}
	}
	return buf.String()
}

func (c *configs) Set(path string) error {
	path, err := file.NormalizePath(path)
	if err != nil {
		return err
	}
	yml, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := echo.ParseConfigs(yml)
	if err != nil {
		return err
	}
	*c = out
	return nil
}

func (c *configs) SetConfig(m interface{}) error {
	yml, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	out, err := echo.ParseConfigs(yml)
	if err != nil {
		return err
	}
	*c = out
	return nil
}
