// +build integ
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

package pilot

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	NMinusOne   = "1.9.0"
	NMinusTwo   = "1.8.3"
	NMinusThree = "1.7.6"
	NMinusFour  = "1.6.11"
)

type revisionedNamespace struct {
	revision  string
	namespace namespace.Instance
}

// TestMultiVersionRevision tests traffic between data planes running under differently versioned revisions
// should test all possible revisioned namespace pairings to test traffic between all versions
func TestMultiVersionRevision(t *testing.T) {
	framework.NewTest(t).
		RequiresSingleCluster().
		Features("installation.upgrade").
		Run(func(t framework.TestContext) {
			skipIfK8sVersionUnsupported(t)

			// keep these at the latest patch version of each minor version
			installVersions := []string{NMinusOne, NMinusTwo, NMinusThree, NMinusFour}

			// keep track of applied configurations and clean up after the test
			configs := make(map[string]string)
			t.ConditionalCleanup(func() {
				for _, config := range configs {
					t.Config().DeleteYAML("istio-system", config)
				}
			})

			revisionedNamespaces := []revisionedNamespace{}
			for _, v := range installVersions {
				installRevisionOrFail(t, v, configs)

				// create a namespace pointed to the revisioned control plane we just installed
				rev := strings.ReplaceAll(v, ".", "-")
				ns, err := namespace.New(t, namespace.Config{
					Prefix:   fmt.Sprintf("revision-%s", rev),
					Inject:   true,
					Revision: rev,
				})
				if err != nil {
					t.Fatalf("failed to created revisioned namespace: %v", err)
				}
				revisionedNamespaces = append(revisionedNamespaces, revisionedNamespace{
					revision:  rev,
					namespace: ns,
				})
			}

			// create an echo instance in each revisioned namespace, all these echo
			// instances will be injected with proxies from their respective versions
			builder := echoboot.NewBuilder(t)

			for _, ns := range revisionedNamespaces {
				builder = builder.WithConfig(echo.Config{
					Service:   fmt.Sprintf("revision-%s", ns.revision),
					Namespace: ns.namespace,
					Ports: []echo.Port{
						{
							Name:         "http",
							Protocol:     protocol.HTTP,
							InstancePort: 8000,
						},
						{
							Name:         "tcp",
							Protocol:     protocol.TCP,
							InstancePort: 9000,
						},
						{
							Name:         "grpc",
							Protocol:     protocol.GRPC,
							InstancePort: 9090,
						},
					},
				})
			}
			instances := builder.BuildOrFail(t)
			// add an existing pod from apps to the rotation to avoid an extra deployment
			instances = append(instances, apps.PodA[0])

			testAllEchoCalls(t, instances)
		})
}

// testAllEchoCalls takes list of revisioned namespaces and generates list of echo calls covering
// communication between every pair of namespaces
func testAllEchoCalls(t framework.TestContext, echoInstances []echo.Instance) {
	trafficTypes := []string{"http", "tcp", "grpc"}
	for _, source := range echoInstances {
		for _, dest := range echoInstances {
			if source == dest {
				continue
			}
			for _, trafficType := range trafficTypes {
				t.NewSubTest(fmt.Sprintf("%s-%s->%s", trafficType, source.Config().Service, dest.Config().Service)).
					Run(func(t framework.TestContext) {
						retry.UntilSuccessOrFail(t, func() error {
							resp, err := source.Call(echo.CallOptions{
								Target:   dest,
								PortName: trafficType,
							})
							if err != nil {
								return err
							}
							return resp.CheckOK()
						}, retry.Delay(time.Millisecond*150))
					})
			}
		}
	}
}

// installRevisionOrFail takes an Istio version and installs a revisioned control plane running that version
// provided istio version must be present in tests/integration/pilot/testdata/upgrade for the installation to succeed
func installRevisionOrFail(t framework.TestContext, version string, configs map[string]string) {
	config, err := ReadInstallFile(fmt.Sprintf("%s-install.yaml", version))
	if err != nil {
		t.Fatalf("could not read installation config: %v", err)
	}
	configs[version] = config
	if err := t.Config().ApplyYAMLNoCleanup(i.Settings().SystemNamespace, config); err != nil {
		t.Fatal(err)
	}
}

// ReadInstallFile reads a tar compress installation file from the embedded
func ReadInstallFile(f string) (string, error) {
	b, err := ioutil.ReadFile(filepath.Join("testdata/upgrade", f+".tar"))
	if err != nil {
		return "", err
	}
	tr := tar.NewReader(bytes.NewBuffer(b))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return "", err
		}
		if hdr.Name != f {
			continue
		}
		contents, err := ioutil.ReadAll(tr)
		if err != nil {
			return "", err
		}
		return string(contents), nil
	}
	return "", fmt.Errorf("file not found")
}

// skipIfK8sVersionUnsupported skips the test if we're running on a k8s version that is not expected to work
// with any of the revision versions included in the test (i.e. istio 1.7 not supported on k8s 1.15)
func skipIfK8sVersionUnsupported(t framework.TestContext) {
	if !t.Clusters().Default().MinKubeVersion(1, 16) {
		t.Skipf("k8s version not supported for %s (<%s)", t.Name(), "1.16")
	}
}
