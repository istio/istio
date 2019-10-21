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

package cmd

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schemas"
)

// sortedConfigStore lets us facade any ConfigStore (such as memory.Make()'s) providing
// a stable List() which helps with testing `istioctl get` output.
type sortedConfigStore struct {
	store model.ConfigStore
}

type testCase struct {
	configs []model.Config
	args    []string

	// Typically use one of the three
	expectedOutput string         // Expected constant output
	expectedRegexp *regexp.Regexp // Expected regexp output
	goldenFilename string         // Expected output stored in golden file

	wantException bool
}

var (
	testGateways = []model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Name:      "bookinfo-gateway",
				Namespace: "default",
				Type:      schemas.Gateway.Type,
				Group:     schemas.Gateway.Group,
				Version:   schemas.Gateway.Version,
			},
			Spec: &networking.Gateway{
				Selector: map[string]string{"istio": "ingressgateway"},
				Servers: []*networking.Server{
					{
						Port: &networking.Port{
							Number:   80,
							Name:     "http",
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
				Type:      schemas.VirtualService.Type,
				Group:     schemas.VirtualService.Group,
				Version:   schemas.VirtualService.Version,
			},
			Spec: &networking.VirtualService{
				Hosts:    []string{"*"},
				Gateways: []string{"bookinfo-gateway"},
				Http: []*networking.HTTPRoute{
					{
						Match: []*networking.HTTPMatchRequest{
							{
								Uri: &networking.StringMatch{
									MatchType: &networking.StringMatch_Exact{Exact: "/productpage"},
								},
							},
							{
								Uri: &networking.StringMatch{
									MatchType: &networking.StringMatch_Exact{Exact: "/login"},
								},
							},
							{
								Uri: &networking.StringMatch{
									MatchType: &networking.StringMatch_Exact{Exact: "/logout"},
								},
							},
							{
								Uri: &networking.StringMatch{
									MatchType: &networking.StringMatch_Prefix{Prefix: "/api/v1/products"},
								},
							},
						},
						Route: []*networking.HTTPRouteDestination{
							{
								Destination: &networking.Destination{
									Host: "productpage",
									Port: &networking.PortSelector{
										Number: 80,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	testDestinationRules = []model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Name:      "googleapis",
				Namespace: "default",
				Type:      schemas.DestinationRule.Type,
				Group:     schemas.DestinationRule.Group,
				Version:   schemas.DestinationRule.Version,
			},
			Spec: &networking.DestinationRule{
				Host: "*.googleapis.com",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.TLSSettings{
						Mode: networking.TLSSettings_SIMPLE,
					},
				},
			},
		},
	}

	testServiceEntries = []model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Name:      "googleapis",
				Namespace: "default",
				Type:      schemas.ServiceEntry.Type,
				Group:     schemas.ServiceEntry.Group,
				Version:   schemas.ServiceEntry.Version,
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{"*.googleapis.com"},
				Ports: []*networking.Port{
					{
						Name:     "https",
						Number:   443,
						Protocol: "HTTP",
					},
				},
			},
		},
	}
)

func TestGet(t *testing.T) {
	cases := []testCase{
		{
			configs: []model.Config{},
			args:    strings.Split("get destinationrules", " "),
			expectedOutput: `Command "get" is deprecated, Use ` + "`kubectl get`" + ` instead (see https://kubernetes.io/docs/tasks/tools/install-kubectl)
No resources found.
`,
		},
		{
			configs: testGateways,
			args:    strings.Split("get gateways -n default", " "),
			expectedOutput: `Command "get" is deprecated, Use ` + "`kubectl get`" + ` instead (see https://kubernetes.io/docs/tasks/tools/install-kubectl)
GATEWAY NAME       HOSTS     NAMESPACE   AGE
bookinfo-gateway   *         default     0s
`,
		},
		{
			configs: testVirtualServices,
			args:    strings.Split("get virtualservices -n default", " "),
			expectedOutput: `Command "get" is deprecated, Use ` + "`kubectl get`" + ` instead (see https://kubernetes.io/docs/tasks/tools/install-kubectl)
VIRTUAL-SERVICE NAME   GATEWAYS           HOSTS     #HTTP     #TCP      NAMESPACE   AGE
bookinfo               bookinfo-gateway   *             1        0      default     0s
`,
		},
		{
			configs: []model.Config{},
			args:    strings.Split("get all", " "),
			expectedOutput: `Command "get" is deprecated, Use ` + "`kubectl get`" + ` instead (see https://kubernetes.io/docs/tasks/tools/install-kubectl)
No resources found.
`,
		},
		{
			configs: testDestinationRules,
			args:    strings.Split("get destinationrules -n default", " "),
			expectedOutput: `Command "get" is deprecated, Use ` + "`kubectl get`" + ` instead (see https://kubernetes.io/docs/tasks/tools/install-kubectl)
DESTINATION-RULE NAME   HOST               SUBSETS   NAMESPACE   AGE
googleapis              *.googleapis.com             default     0s
`,
		},
		{
			configs: testServiceEntries,
			args:    strings.Split("get serviceentries -n default", " "),
			expectedOutput: `Command "get" is deprecated, Use ` + "`kubectl get`" + ` instead (see https://kubernetes.io/docs/tasks/tools/install-kubectl)
SERVICE-ENTRY NAME   HOSTS              PORTS      NAMESPACE   AGE
googleapis           *.googleapis.com   HTTP/443   default     0s
`,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func TestCreate(t *testing.T) {
	cases := []testCase{
		{ // invalid doesn't provide -f filename
			configs:        []model.Config{},
			args:           strings.Split("create virtualservice", " "),
			expectedRegexp: regexp.MustCompile("^Command \"create\" is deprecated, Use `kubectl create` instead (see https://kubernetes.io/docs/tasks/tools/install-kubectl)*"), // nolint: lll
			wantException:  true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func TestReplace(t *testing.T) {
	cases := []testCase{
		{ // invalid doesn't provide -f
			configs:        []model.Config{},
			args:           strings.Split("replace virtualservice", " "),
			expectedRegexp: regexp.MustCompile("^Command \"replace\" is deprecated, Use `kubectl apply` instead (see https://kubernetes.io/docs/tasks/tools/install-kubectl)*"), // nolint: lll
			wantException:  true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func TestDelete(t *testing.T) {
	cases := []testCase{
		{
			configs:        []model.Config{},
			args:           strings.Split("delete all foo", " "),
			expectedRegexp: regexp.MustCompile("^Command \"delete\" is deprecated, Use `kubectl delete` instead (see https://kubernetes.io/docs/tasks/tools/install-kubectl)*"), // nolint: lll
			wantException:  true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func TestBadParse(t *testing.T) {
	// unknown flags should be a command parse
	rootCmd := GetRootCmd([]string{"--unknown-flag"})
	fErr := rootCmd.Execute()

	switch fErr.(type) {
	case CommandParseError:
		// do nothing
	default:
		t.Errorf("Expected a CommandParseError, but got %q.", fErr)
	}

	// we should propagate to subcommands
	rootCmd = GetRootCmd([]string{"x", "analyze", "--unknown-flag"})
	fErr = rootCmd.Execute()

	switch fErr.(type) {
	case CommandParseError:
		// do nothing
	default:
		t.Errorf("Expected a CommandParseError, but got %q.", fErr)
	}

	// all of the subcommands
	rootCmd = GetRootCmd([]string{"authn", "tls-check", "--unknown-flag"})
	fErr = rootCmd.Execute()

	switch fErr.(type) {
	case CommandParseError:
		// do nothing
	default:
		t.Errorf("Expected a CommandParseError, but got %q.", fErr)
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

func (cs sortedConfigStore) Create(config model.Config) (string, error) {
	return cs.store.Create(config)
}

func (cs sortedConfigStore) Get(typ, name, namespace string) *model.Config {
	return cs.store.Get(typ, name, namespace)
}

func (cs sortedConfigStore) Update(config model.Config) (string, error) {
	return cs.store.Update(config)
}
func (cs sortedConfigStore) Delete(typ, name, namespace string) error {
	return cs.store.Delete(typ, name, namespace)
}

func (cs sortedConfigStore) ConfigDescriptor() schema.Set {
	return cs.store.ConfigDescriptor()
}

func (cs sortedConfigStore) Version() string {
	return cs.store.Version()
}
func (cs sortedConfigStore) GetResourceAtVersion(version string, key string) (resourceVersion string, err error) {
	return cs.store.GetResourceAtVersion(version, key)
}

// List() is a facade that always returns cs.store items sorted by name/namespace
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

func verifyOutput(t *testing.T, c testCase) {
	t.Helper()

	// Override the client factory used by main.go
	clientFactory = mockClientFactoryGenerator(c.configs)

	var out bytes.Buffer
	rootCmd := GetRootCmd(c.args)
	rootCmd.SetOutput(&out)

	file = "" // Clear, because we re-use

	fErr := rootCmd.Execute()
	output := out.String()

	if c.expectedOutput != "" && c.expectedOutput != output {
		t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q",
			strings.Join(c.args, " "), output, c.expectedOutput)
	}

	if c.expectedRegexp != nil && !c.expectedRegexp.MatchString(output) {
		t.Fatalf("Output didn't match for 'istioctl %s'\n got %v\nwant: %v",
			strings.Join(c.args, " "), output, c.expectedRegexp)
	}

	if c.goldenFilename != "" {
		util.CompareContent([]byte(output), c.goldenFilename, t)
	}

	if c.wantException {
		if fErr == nil {
			t.Fatalf("Wanted an exception for 'istioctl %s', didn't get one, output was %q",
				strings.Join(c.args, " "), output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Unwanted exception for 'istioctl %s': %v", strings.Join(c.args, " "), fErr)
		}
	}
}
