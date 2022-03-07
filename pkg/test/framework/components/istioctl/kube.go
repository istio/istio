//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package istioctl

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"time"

	"istio.io/istio/istioctl/cmd"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

// We cannot invoke the istioctl library concurrently due to the number of global variables
// https://github.com/istio/istio/issues/37324
var invokeMutex sync.Mutex

type kubeComponent struct {
	config     Config
	kubeconfig string
}

// Filenamer is an interface to avoid importing kubecluster package, instead build our own interface
// to extract kube context
type Filenamer interface {
	Filename() string
}

func newKube(ctx resource.Context, config Config) (Instance, error) {
	fn, ok := ctx.Clusters().GetOrDefault(config.Cluster).(Filenamer)
	if !ok {
		return nil, fmt.Errorf("cluster does not support fetching kube config")
	}
	n := &kubeComponent{
		config:     config,
		kubeconfig: fn.Filename(),
	}

	return n, nil
}

// Invoke implements WaitForConfigs
func (c *kubeComponent) WaitForConfig(defaultNamespace string, configs string) error {
	cfgs, _, err := crd.ParseInputs(configs)
	if err != nil {
		return fmt.Errorf("failed to parse input: %v", err)
	}
	for _, cfg := range cfgs {
		ns := cfg.Namespace
		if ns == "" {
			ns = defaultNamespace
		}
		// TODO(https://github.com/istio/istio/issues/37148) increase timeout. Right now it fails often, so
		// set it to low timeout to reduce impact
		if out, stderr, err := c.Invoke([]string{"x", "wait", "-v", "--timeout=5s", cfg.GroupVersionKind.Kind, cfg.Name + "." + ns}); err != nil {
			return fmt.Errorf("wait: %v\nout: %v\nerr: %v", err, out, stderr)
		}
	}
	return nil
}

// Invoke implements Instance
func (c *kubeComponent) Invoke(args []string) (string, string, error) {
	cmdArgs := append([]string{
		"--kubeconfig",
		c.kubeconfig,
	}, args...)

	var out bytes.Buffer
	var err bytes.Buffer

	start := time.Now()

	invokeMutex.Lock()
	rootCmd := cmd.GetRootCmd(cmdArgs)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&err)
	fErr := rootCmd.Execute()
	invokeMutex.Unlock()

	scopes.Framework.Infof("istioctl (%v): completed after %.4fs", args, time.Since(start).Seconds())

	if err.String() != "" {
		scopes.Framework.Infof("istioctl error: %v", strings.TrimSpace(err.String()))
	}
	return out.String(), err.String(), fErr
}

// InvokeOrFail implements Instance
func (c *kubeComponent) InvokeOrFail(t test.Failer, args []string) (string, string) {
	output, stderr, err := c.Invoke(args)
	if err != nil {
		t.Logf("Unwanted exception for 'istioctl %s': %v", strings.Join(args, " "), err)
		t.Logf("Output:\n%v", output)
		t.Logf("Error:\n%v", stderr)
		t.FailNow()
	}
	return output, stderr
}
