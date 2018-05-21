// Copyright 2017 Istio Authors
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

package main

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	multierror "github.com/hashicorp/go-multierror"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/api/routing/v1alpha1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
)

// sortedConfigStore lets us facade any ConfigStore (such as memory.Make()'s) providing
// a stable List() which helps with testing `istioctl get` output.
type sortedConfigStore struct {
	store model.ConfigStore
}

const (
	istioAPIVersion = "v1alpha2"
)

var (
	testRouteRules = []model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Name:      "d",
				Namespace: "default",
				Type:      model.RouteRule.Type,
				Group:     model.RouteRule.Group,
				Version:   model.RouteRule.Version,
			},
			Spec: &v1alpha1.RouteRule{
				Precedence: 2,
				Destination: &v1alpha1.IstioService{
					Name: "d",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "b",
				Namespace: "default",
				Type:      model.RouteRule.Type,
				Group:     model.RouteRule.Group,
				Version:   model.RouteRule.Version,
			},
			Spec: &v1alpha1.RouteRule{
				Precedence: 3,
				Destination: &v1alpha1.IstioService{
					Name: "b",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "c",
				Namespace: "istio-system",
				Type:      model.RouteRule.Type,
				Group:     model.RouteRule.Group,
				Version:   model.RouteRule.Version,
			},
			Spec: &v1alpha1.RouteRule{
				Precedence: 2,
				Destination: &v1alpha1.IstioService{
					Name: "c",
				},
			},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "a",
				Namespace: "default",
				Type:      model.RouteRule.Type,
				Group:     model.RouteRule.Group,
				Version:   model.RouteRule.Version,
			},
			Spec: &v1alpha1.RouteRule{
				Precedence: 1,
				Destination: &v1alpha1.IstioService{
					Name: "a",
				},
			},
		},
	}

	testGateways = []model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Name:      "bookinfo-gateway",
				Namespace: "default",
				Type:      model.Gateway.Type,
				Group:     model.Gateway.Group,
				Version:   model.Gateway.Version,
			},
			Spec: &networking.Gateway{
				Selector: map[string]string{"istio": "ingressgateway"},
				Servers: []*networking.Server{
					{
						Port: &networking.Port{
							Number:   80,
							Protocol: "HTTP",
						},
						Hosts: []string{"*"},
					},
				},
			},
		},
	}

	testVirtualServices = []model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Name:      "bookinfo",
				Namespace: "default",
				Type:      model.VirtualService.Type,
				Group:     model.VirtualService.Group,
				Version:   model.VirtualService.Version,
			},
			Spec: &networking.VirtualService{
				Hosts:    []string{"*"},
				Gateways: []string{"bookinfo-gateway"},
				Http: []*networking.HTTPRoute{
					{
						Match: []*networking.HTTPMatchRequest{
							{
								Uri: &networking.StringMatch{
									&networking.StringMatch_Exact{"/productpage"},
								},
							},
							{
								Uri: &networking.StringMatch{
									&networking.StringMatch_Exact{"/login"},
								},
							},
							{
								Uri: &networking.StringMatch{
									&networking.StringMatch_Exact{"/logout"},
								},
							},
							{
								Uri: &networking.StringMatch{
									&networking.StringMatch_Prefix{"/api/v1/products"},
								},
							},
						},
						Route: []*networking.DestinationWeight{
							{
								Destination: &networking.Destination{
									Host: "productpage",
									Port: &networking.PortSelector{
										Port: &networking.PortSelector_Number{80},
									},
								},
							},
						},
					},
				},
			},
		},
	}
)

func TestGet(t *testing.T) {
	cases := []struct {
		configs       []model.Config
		args          []string
		wantOutput    string
		wantError     *regexp.Regexp
		wantException bool
	}{
		{ // case 0
			[]model.Config{},
			[]string{"routerules"},
			`No resources found.
`,
			regexp.MustCompile("^$"),
			false,
		},
		{ // case 1
			testRouteRules,
			[]string{"routerules"},
			`NAME      KIND                        NAMESPACE
a         RouteRule.config.v1alpha2   default
b         RouteRule.config.v1alpha2   default
c         RouteRule.config.v1alpha2   istio-system
d         RouteRule.config.v1alpha2   default
`,
			regexp.MustCompile("^$"),
			false,
		},
		{ // case 2
			testGateways,
			[]string{"gateways"},
			`NAME               KIND                          NAMESPACE
bookinfo-gateway   Gateway.networking.v1alpha3   default
`,
			regexp.MustCompile("^$"),
			false,
		},
		{ // case 3
			testVirtualServices,
			[]string{"virtualservices"},
			`NAME       KIND                                 NAMESPACE
bookinfo   VirtualService.networking.v1alpha3   default
`,
			regexp.MustCompile("^$"),
			false,
		},
		{
			[]model.Config{},
			[]string{"invalid"},
			"",
			regexp.MustCompile("^Usage:.*"),
			true, // "istioctl get invalid" should fail
		},
	}

	for i, c := range cases {
		// Override the client factory used by main.go
		clientFactory = mockClientFactoryGenerator(c.configs)

		// run getCmd capturing output
		stdOutput, errOutput, fErr := captureOutput(
			func() error {
				err := rootCmd.PersistentPreRunE(getCmd, c.args)
				if err != nil {
					return multierror.Prefix(err, "Could not prerun GET command")
				}
				err = getCmd.RunE(getCmd, c.args)
				if err != nil {
					return multierror.Prefix(err, "Could not run GET command")
				}
				return nil
			})

		if c.wantOutput != stdOutput {
			t.Errorf("Stdout didn't match for case %d \"istioctl get %v\": Expected %q, got %q.  Stderr was %q",
				i, strings.Join(c.args, " "),
				c.wantOutput, stdOutput, errOutput)
		}

		if !c.wantError.MatchString(errOutput) {
			t.Errorf("Stderr didn't match for case %d %v: Expected %v, got %q", i, c.args, c.wantError, errOutput)
		}

		if c.wantException {
			if fErr == nil {
				t.Errorf("Wanted an exception for case %d %v, didn't get one, output was %q", i, c.args, stdOutput)
			}
		} else {
			if fErr != nil {
				t.Errorf("Unwanted exception for case %d %v: %v", i, c.args, fErr)
			}
		}
	}
}

func TestCreate(t *testing.T) {
	cases := []struct {
		configs       []model.Config
		args          []string
		filename      string
		wantOutput    *regexp.Regexp
		wantError     *regexp.Regexp
		wantException bool
	}{
		{ // case 0 -- invalid doesn't provide -f filename
			[]model.Config{},
			[]string{"routerules"},
			"",
			regexp.MustCompile("^$"),
			regexp.MustCompile("^Usage:.*"),
			true,
		},
		{ // case 1
			[]model.Config{},
			[]string{},
			"convert/testdata/v1alpha1/route-rule-80-20.yaml",
			regexp.MustCompile("^Created config route-rule/default/route-rule-80-20.*"),
			regexp.MustCompile("^$"),
			false,
		},
	}

	for i, c := range cases {
		// Override the client factory used by main.go
		clientFactory = mockClientFactoryGenerator(c.configs)

		// set the filename
		file = c.filename

		// run getCmd capturing output
		stdOutput, errOutput, fErr := captureOutput(
			func() error {
				err := rootCmd.PersistentPreRunE(postCmd, c.args)
				if err != nil {
					return multierror.Prefix(err, "Could not prerun CREATE command")
				}
				err = postCmd.RunE(postCmd, c.args)
				if err != nil {
					return multierror.Prefix(err, "Could not run CREATE command")
				}
				return nil
			})

		if !c.wantOutput.MatchString(stdOutput) {
			t.Errorf("Stdout didn't match for create case %d \"istioctl create %v\": Expected %v, got %q.  Stderr was %q",
				i, strings.Join(c.args, " "),
				c.wantOutput, stdOutput, errOutput)
		}

		if !c.wantError.MatchString(errOutput) {
			t.Errorf("Stderr didn't match for create case %d %v: Expected %v, got %q", i, c.args, c.wantError, errOutput)
		}

		if c.wantException {
			if fErr == nil {
				t.Errorf("Wanted an exception for case %d %v, didn't get one, output was %q", i, c.args, stdOutput)
			}
		} else {
			if fErr != nil {
				t.Errorf("Unwanted exception for case %d %v: %v", i, c.args, fErr)
			}
		}
	}
}

func TestReplace(t *testing.T) {
	cases := []struct {
		configs       []model.Config
		args          []string
		filename      string
		wantOutput    string
		wantError     *regexp.Regexp
		wantException bool
	}{
		{ // case 0 -- invalid doesn't provide -f
			[]model.Config{},
			[]string{"routerules"},
			"",
			"",
			regexp.MustCompile("^Usage:.*"),
			true,
		},
	}

	for i, c := range cases {
		// Override the client factory used by main.go
		clientFactory = mockClientFactoryGenerator(c.configs)

		// run getCmd capturing output
		stdOutput, errOutput, fErr := captureOutput(
			func() error {
				err := rootCmd.PersistentPreRunE(putCmd, c.args)
				if err != nil {
					return multierror.Prefix(err, "Could not prerun REPLACE command")
				}
				err = putCmd.RunE(putCmd, c.args)
				if err != nil {
					return multierror.Prefix(err, "Could not run REPLACE command")
				}
				return nil
			})

		if c.wantOutput != stdOutput {
			t.Errorf("Stdout didn't match for case %d \"istioctl replace %v\": Expected %q, got %q.  Stderr was %q",
				i, strings.Join(c.args, " "),
				c.wantOutput, stdOutput, errOutput)
		}

		if !c.wantError.MatchString(errOutput) {
			t.Errorf("Stderr didn't match for case %d %v: Expected %v, got %q", i, c.args, c.wantError, errOutput)
		}

		if c.wantException {
			if fErr == nil {
				t.Errorf("Wanted an exception for case %d %v, didn't get one, output was %q", i, c.args, stdOutput)
			}
		} else {
			if fErr != nil {
				t.Errorf("Unwanted exception for case %d %v: %v", i, c.args, fErr)
			}
		}
	}
}

func TestDelete(t *testing.T) {
	cases := []struct {
		configs       []model.Config
		args          []string
		filename      string
		wantOutput    string
		wantError     *regexp.Regexp
		wantException bool
	}{
		{ // case 0
			[]model.Config{},
			[]string{"routerule", "unknown"},
			"",
			"",
			regexp.MustCompile("^$"),
			true,
		},
		{ // case 1
			testRouteRules,
			[]string{"routerule", "a"},
			"",
			`Deleted config: routerule a
`,
			regexp.MustCompile("^$"),
			false,
		},
		{ // case 2 - delete by filename of istio config which doesn't exist
			testRouteRules,
			[]string{},
			"convert/testdata/v1alpha1/route-rule-80-20.yaml",
			"",
			regexp.MustCompile("^$"),
			true,
		},
	}

	for i, c := range cases {
		// Override the client factory used by main.go
		clientFactory = mockClientFactoryGenerator(c.configs)

		// set the filename
		file = c.filename

		// run getCmd capturing output
		stdOutput, errOutput, fErr := captureOutput(
			func() error {
				err := rootCmd.PersistentPreRunE(deleteCmd, c.args)
				if err != nil {
					return multierror.Prefix(err, "Could not prerun DELETE command")
				}
				err = deleteCmd.RunE(deleteCmd, c.args)
				if err != nil {
					return multierror.Prefix(err, "Could not run DELETE command")
				}
				return nil
			})

		if c.wantOutput != stdOutput {
			t.Errorf("Stdout didn't match for delete case %d \"istioctl delete %v\": Expected %q, got %q.  Stderr was %q",
				i, strings.Join(c.args, " "),
				c.wantOutput, stdOutput, errOutput)
		}

		if !c.wantError.MatchString(errOutput) {
			t.Errorf("Stderr didn't match for delete case %d %v: Expected %v, got %q", i, c.args, c.wantError, errOutput)
		}

		if c.wantException {
			if fErr == nil {
				t.Errorf("Wanted an exception for case %d %v, didn't get one, output was %q", i, c.args, stdOutput)
			}
		} else {
			if fErr != nil {
				t.Errorf("Unwanted exception for case %d %v: %v", i, c.args, fErr)
			}
		}
	}
}

// mockClientFactoryGenerator creates a factory for model.ConfigStore preloaded with data
func mockClientFactoryGenerator(configs []model.Config) func() (model.ConfigStore, error) {
	outFactory := func() (model.ConfigStore, error) {
		// Initialize the real client to get the supported config types
		realClient, err := newClient()
		if err != nil {
			return nil, err
		}

		// Initialize memory based model.ConfigStore with configs
		outConfig := memory.Make(realClient.ConfigDescriptor())
		for _, config := range configs {
			if _, err := outConfig.Create(config); err != nil {
				return nil, err
			}
		}

		// Wrap the memory ConfigStore so List() is sorted
		return sortedConfigStore{store: outConfig}, nil
	}

	return outFactory
}

// captureOutput invokes f capturing the output sent to stderr and stdout
func captureOutput(f func() error) (string, string, error) {
	origStdout := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	origStderr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	errF := f()

	outChannel := make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, rOut)
		outChannel <- buf.String()
	}()

	errChannel := make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, rErr)
		errChannel <- buf.String()
	}()

	wOut.Close()
	os.Stdout = origStdout
	capturedOutput := <-outChannel

	wErr.Close()
	os.Stderr = origStderr
	capturedError := <-errChannel

	return capturedOutput, capturedError, errF
}

func (cs sortedConfigStore) Create(config model.Config) (string, error) {
	return cs.store.Create(config)
}

func (cs sortedConfigStore) Get(typ, name, namespace string) (*model.Config, bool) {
	return cs.store.Get(typ, name, namespace)
}

func (cs sortedConfigStore) Update(config model.Config) (string, error) {
	return cs.store.Update(config)
}
func (cs sortedConfigStore) Delete(typ, name, namespace string) error {
	return cs.store.Delete(typ, name, namespace)
}

func (cs sortedConfigStore) ConfigDescriptor() model.ConfigDescriptor {
	return cs.store.ConfigDescriptor()
}

func (cs sortedConfigStore) List(typ, namespace string) ([]model.Config, error) {
	out, err := cs.store.List(typ, namespace)
	if err != nil {
		return out, err
	}

	// Sort by name, namespace
	sort.Slice(out, func(i, j int) bool {
		iName := out[i].ConfigMeta.Name
		jName := out[j].ConfigMeta.Name
		if iName == jName {
			return out[i].ConfigMeta.Namespace < out[j].ConfigMeta.Namespace
		}
		return iName < jName
	})

	return out, nil
}
