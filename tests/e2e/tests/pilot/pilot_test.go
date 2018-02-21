// Copyright 2018 Istio Authors
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
	"flag"
	"os"
	"strconv"
	"testing"

	"fmt"

	"istio.io/istio/pkg/log"
	tutil "istio.io/istio/tests/e2e/tests/pilot/util"
)

const (
	authTestName   = "Auth"
	noAuthTestName = "NoAuth"
)

// AuthMode is an enumeration for the auth mode flag.
type authMode string

const (
	authModeEnable  authMode = "enable"
	authModeDisable authMode = "disable"
	authModeBoth    authMode = "both"
)

var (
	defaultConfig = tutil.NewInfra()

	// Enable/disable auth, or run both for the tests.
	authmode string
	verbose  bool
)

func init() {
	flag.StringVar(&defaultConfig.Hub, "hub", defaultConfig.Hub, "Docker hub")
	flag.StringVar(&defaultConfig.Tag, "tag", defaultConfig.Tag, "Docker tag")
	flag.StringVar(&defaultConfig.IstioNamespace, "ns", defaultConfig.IstioNamespace,
		"Namespace in which to install Istio components (empty to create/delete temporary one)")
	flag.StringVar(&defaultConfig.Namespace, "n", defaultConfig.Namespace,
		"Namespace in which to install the applications (empty to create/delete temporary one)")
	flag.StringVar(&defaultConfig.Registry, "registry", defaultConfig.Registry, "Pilot registry")
	flag.BoolVar(&verbose, "verbose", false, "Debug level noise from proxies")
	flag.BoolVar(&defaultConfig.CheckLogs, "logs", defaultConfig.CheckLogs,
		"Validate pod logs (expensive in long-running tests)")

	flag.StringVar(&defaultConfig.KubeConfig, "kubeconfig", defaultConfig.KubeConfig,
		"kube config file (missing or empty file makes the test use in-cluster kube config instead)")
	flag.IntVar(&defaultConfig.TestCount, "count", defaultConfig.TestCount, "Number of times to run each test")
	flag.StringVar(&authmode, "auth", string(authModeBoth),
		fmt.Sprintf("Auth mode for the tests (Choose from %s, %s, %s)", authModeEnable, authModeDisable, authModeBoth))
	flag.BoolVar(&defaultConfig.Mixer, "mixer", defaultConfig.Mixer, "Enable / disable mixer.")
	flag.BoolVar(&defaultConfig.V1alpha1, "v1alpha1", defaultConfig.V1alpha1, "Enable / disable v1alpha1 routing rules.")
	flag.BoolVar(&defaultConfig.V1alpha2, "v1alpha2", defaultConfig.V1alpha2, "Enable / disable v1alpha2 routing rules.")
	flag.StringVar(&defaultConfig.ErrorLogsDir, "errorlogsdir", defaultConfig.ErrorLogsDir,
		"Store per pod logs as individual files in specific directory instead of writing to stderr.")
	flag.StringVar(&defaultConfig.CoreFilesDir, "core-files-dir", defaultConfig.CoreFilesDir,
		"Copy core files to this directory on the Kubernetes node machine.")

	// If specified, only run one test
	flag.StringVar(&defaultConfig.SelectedTest, "testtype", defaultConfig.SelectedTest,
		"Select test to run (default is all tests)")

	flag.BoolVar(&defaultConfig.UseAutomaticInjection, "use-sidecar-injector", defaultConfig.UseAutomaticInjection,
		"Use automatic sidecar injector")
	flag.BoolVar(&defaultConfig.UseAdmissionWebhook, "use-admission-webhook", defaultConfig.UseAdmissionWebhook,
		"Use k8s external admission webhook for config validation")

	flag.StringVar(&defaultConfig.AdmissionServiceName, "admission-service-name", defaultConfig.AdmissionServiceName,
		"Name of admission webhook service name")

	flag.IntVar(&defaultConfig.DebugPort, "debugport", defaultConfig.DebugPort, "Debugging port")

	flag.BoolVar(&defaultConfig.DebugImagesAndMode, "debug", defaultConfig.DebugImagesAndMode,
		"Use debug images and mode (false for prod)")
	flag.BoolVar(&defaultConfig.SkipCleanup, "skip-cleanup", defaultConfig.SkipCleanup,
		"Debug, skip clean up")
	flag.BoolVar(&defaultConfig.SkipCleanupOnFailure, "skip-cleanup-on-failure", defaultConfig.SkipCleanupOnFailure,
		"Debug, skip clean up on failure")
}

func setup(config *tutil.Infra, t *testing.T) {
	// TODO(nmittler): Restore this tutil.Tlog("Deploying infrastructure", spew.Sdump(config))
	if config.Err = config.Setup(); config.Err != nil {
		t.Fatal(config.Err)
	}
}

func teardown(config *tutil.Infra) {
	config.Teardown()
}

func TestPilot(t *testing.T) {
	if verbose {
		defaultConfig.Verbosity = 3
	}

	// Only run the tests if the user has defined the KUBECONFIG environment variable.
	if defaultConfig.KubeConfig == "" {
		t.Skip("Env variable KUBECONFIG not set. Skipping tests")
	}

	if defaultConfig.Hub == "" {
		t.Skip("HUB not specified. Skipping tests")
	}

	if defaultConfig.Tag == "" {
		t.Skip("TAG not specified. Skipping tests")
	}

	if defaultConfig.Namespace != "" && authMode(authmode) == authModeBoth {
		t.Skipf("When namespace(=%s) is specified, auth mode(=%s) must be one of enable or disable. Skipping tests.",
			defaultConfig.Namespace, authmode)
	}

	noAuthInfra := defaultConfig
	authInfra := noAuthInfra.CopyWithDefaultAuth()

	switch authMode(authmode) {
	case authModeEnable:
		doTest(authTestName, authInfra, t)
	case authModeDisable:
		doTest(noAuthTestName, noAuthInfra, t)
	case authModeBoth:
		doTest(noAuthTestName, noAuthInfra, t)
		doTest(authTestName, authInfra, t)
	default:
		t.Fatalf("Unknown auth mode(=%s).", authmode)
	}
}

func doTest(testName string, config *tutil.Infra, t *testing.T) {
	t.Run(testName, func(t *testing.T) {
		defer teardown(config)
		setup(config, t)

		tests := []tutil.Test{
			&http{Infra: config},
			&grpc{Infra: config},
			&tcp{Infra: config},
			&headless{Infra: config},
			&ingress{Infra: config},
			&egressRules{Infra: config},
			&routing{Infra: config},
			&routingToEgress{Infra: config},
			&zipkin{Infra: config},
			&authExclusion{Infra: config},
			&kubernetesExternalNameServices{Infra: config},
		}

		for _, test := range tests {
			// If the user has specified a test, skip all other tests
			if len(config.SelectedTest) > 0 && config.SelectedTest != test.String() {
				continue
			}

			// Run the test the configured number of times.
			for i := 0; i < config.TestCount; i++ {
				testName := test.String()
				if config.TestCount > 1 {
					testName = testName + "_attempt_" + strconv.Itoa(i+1)
				}
				t.Run(testName, func(t *testing.T) {
					if config.Err = test.Setup(); config.Err != nil {
						t.Fatal(config.Err)
					}
					defer test.Teardown()

					if config.Err = test.Run(); config.Err != nil {
						t.Error(config.Err)
					}
				})
			}
		}
	})
}

// TODO(nmittler): convert individual tests over to pure golang tests
func TestMain(m *testing.M) {
	flag.Parse()
	_ = log.Configure(log.NewOptions())

	// Run all tests.
	os.Exit(m.Run())
}
