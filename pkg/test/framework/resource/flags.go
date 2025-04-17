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

package resource

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/config"
	"istio.io/istio/pkg/test/framework/label"
)

var settingsFromCommandLine = DefaultSettings()

// SettingsFromCommandLine returns settings obtained from command-line flags. config.Parse must be called before
// calling this function.
func SettingsFromCommandLine(testID string) (*Settings, error) {
	if !config.Parsed() {
		panic("config.Parse must be called before this function")
	}

	s := settingsFromCommandLine.Clone()
	s.TestID = testID

	f, err := label.ParseSelector(s.SelectorString)
	if err != nil {
		return nil, err
	}
	s.Selector = f

	s.SkipMatcher, err = NewMatcher(s.SkipString)
	if err != nil {
		return nil, err
	}

	// NOTE: not using echo.VM, etc. here to avoid circular dependency.
	if s.SkipVM {
		s.SkipWorkloadClasses = append(s.SkipWorkloadClasses, "vm")
	}
	if s.SkipTProxy {
		s.SkipWorkloadClasses = append(s.SkipWorkloadClasses, "tproxy")
	}
	// Allow passing a single CSV flag as well
	normalized := make(ArrayFlags, 0)
	for _, sk := range s.SkipWorkloadClasses {
		normalized = append(normalized, strings.Split(sk, ",")...)
	}
	s.SkipWorkloadClasses = normalized

	normalizedIPFamilies := make(ArrayFlags, 0)
	for _, ipFamily := range s.IPFamilies {
		normalizedIPFamilies = append(normalizedIPFamilies, strings.Split(ipFamily, ",")...)
	}
	s.IPFamilies = normalizedIPFamilies

	if s.Image.Hub == "" {
		s.Image.Hub = env.HUB.ValueOrDefault("gcr.io/istio-testing")
	}

	if s.Image.Tag == "" {
		s.Image.Tag = env.TAG.ValueOrDefault("latest")
	}

	if s.Image.Variant == "" {
		s.Image.Variant = env.VARIANT.ValueOrDefault("")
	}

	if s.Image.PullPolicy == "" {
		s.Image.PullPolicy = env.PULL_POLICY.ValueOrDefault("Always")
	}

	if s.EchoImage == "" {
		s.EchoImage = env.ECHO_IMAGE.ValueOrDefault("")
	}

	if s.CustomGRPCEchoImage == "" {
		s.CustomGRPCEchoImage = env.GRPC_ECHO_IMAGE.ValueOrDefault("")
	}

	if s.HelmRepo == "" {
		s.HelmRepo = "https://istio-release.storage.googleapis.com/charts"
	}

	if err = validate(s); err != nil {
		return nil, err
	}

	return s, nil
}

// validate checks that user has not passed invalid flag combinations to test framework.
func validate(s *Settings) error {
	if s.FailOnDeprecation && s.NoCleanup {
		return fmt.Errorf("checking for deprecation occurs at cleanup level, thus flags -istio.test.nocleanup and" +
			" -istio.test.deprecation_failure must not be used at the same time")
	}

	if s.Revision != "" {
		if s.Revisions != nil {
			return fmt.Errorf("cannot use --istio.test.revision and --istio.test.revisions at the same time," +
				" --istio.test.revisions will take precedence and --istio.test.revision will be ignored")
		}
		// use Revision as the sole revision in RevVerMap
		s.Revisions = RevVerMap{
			s.Revision: "",
		}
	} else if s.Revisions != nil {
		// TODO(Monkeyanator) remove once existing jobs are migrated to use compatibility flag.
		s.Compatibility = true
	}

	if s.Revisions == nil && s.Compatibility {
		return fmt.Errorf("cannot use --istio.test.compatibility without setting --istio.test.revisions")
	}

	if s.Image.Hub == "" || s.Image.Tag == "" {
		return fmt.Errorf("values for Hub & Tag are not detected. Please supply them through command-line or via environment")
	}

	for _, ipFamily := range s.IPFamilies {
		if ipFamily != "IPv4" && ipFamily != "IPv6" {
			return fmt.Errorf("supported values for --istio.test.IPFamilies are `IPv4`, `IPv6`, `IPv4,IPv6` or `IPv6,IPv4`")
		}
	}
	return nil
}

// init registers the command-line flags that we can exposed for "go test".
func init() {
	log.EnableKlogWithGoFlag()
	flag.StringVar(&settingsFromCommandLine.BaseDir, "istio.test.work_dir", os.TempDir(),
		"Local working directory for creating logs/temp files. If left empty, os.TempDir() is used.")

	var env string
	flag.StringVar(&env, "istio.test.env", "", "Deprecated. This flag does nothing")

	flag.BoolVar(&settingsFromCommandLine.NoCleanup, "istio.test.nocleanup", settingsFromCommandLine.NoCleanup,
		"Do not cleanup resources after test completion")

	flag.BoolVar(&settingsFromCommandLine.CIMode, "istio.test.ci", settingsFromCommandLine.CIMode,
		"Enable CI Mode. Additional logging and state dumping will be enabled.")

	flag.StringVar(&settingsFromCommandLine.SelectorString, "istio.test.select", settingsFromCommandLine.SelectorString,
		"Comma separated list of labels for selecting tests to run (e.g. 'foo,+bar-baz').")

	flag.Var(&settingsFromCommandLine.SkipString, "istio.test.skip",
		"Skip tests matching the regular expression. This follows the semantics of -test.run.")

	flag.Var(&settingsFromCommandLine.SkipWorkloadClasses, "istio.test.skipWorkloads",
		"Skips deploying and using workloads of the given comma-separated classes (e.g. vm, proxyless, etc.)")

	flag.Var(&settingsFromCommandLine.OnlyWorkloadClasses, "istio.test.onlyWorkloads",
		"Skips deploying and using workloads not included in the given comma-separated classes (e.g. vm, proxyless, etc.)")

	flag.IntVar(&settingsFromCommandLine.Retries, "istio.test.retries", settingsFromCommandLine.Retries,
		"Number of times to retry tests")

	flag.BoolVar(&settingsFromCommandLine.StableNamespaces, "istio.test.stableNamespaces", settingsFromCommandLine.StableNamespaces,
		"If set, will use consistent namespace rather than randomly generated. Useful with nocleanup to develop tests.")

	flag.BoolVar(&settingsFromCommandLine.FailOnDeprecation, "istio.test.deprecation_failure", settingsFromCommandLine.FailOnDeprecation,
		"Make tests fail if any usage of deprecated stuff (e.g. Envoy flags) is detected.")

	flag.StringVar(&settingsFromCommandLine.Revision, "istio.test.revision", settingsFromCommandLine.Revision,
		"If set to XXX, overwrite the default namespace label (istio-injection=enabled) with istio.io/rev=XXX.")

	flag.BoolVar(&settingsFromCommandLine.SkipVM, "istio.test.skipVM", settingsFromCommandLine.SkipVM,
		"Skip VM related parts in all tests.")

	flag.BoolVar(&settingsFromCommandLine.SkipTProxy, "istio.test.skipTProxy", settingsFromCommandLine.SkipTProxy,
		"Skip TProxy related parts in all tests.")

	flag.BoolVar(&settingsFromCommandLine.Ambient, "istio.test.ambient", settingsFromCommandLine.Ambient,
		"Indicate the use of ambient mesh.")

	flag.BoolVar(&settingsFromCommandLine.PeerMetadataDiscovery, "istio.test.peer_metadata_discovery", settingsFromCommandLine.PeerMetadataDiscovery,
		"Force the use of peer metadata discovery fallback for metadata exchange")

	flag.BoolVar(&settingsFromCommandLine.AmbientEverywhere, "istio.test.ambient.everywhere", settingsFromCommandLine.AmbientEverywhere,
		"Make Waypoint proxies the default instead of sidecar proxies for all echo apps. Must be used with istio.test.ambient")

	flag.BoolVar(&settingsFromCommandLine.Compatibility, "istio.test.compatibility", settingsFromCommandLine.Compatibility,
		"Transparently deploy echo instances pointing to each revision set in `Revisions`")

	flag.Var(&settingsFromCommandLine.Revisions, "istio.test.revisions", "Istio CP revisions available to the test framework and their corresponding versions.")

	flag.StringVar(&settingsFromCommandLine.Image.Hub, "istio.test.hub", settingsFromCommandLine.Image.Hub,
		"Container registry hub to use")
	flag.StringVar(&settingsFromCommandLine.Image.Tag, "istio.test.tag", settingsFromCommandLine.Image.Tag,
		"Common Container tag to use when deploying container images")
	flag.StringVar(&settingsFromCommandLine.Image.Variant, "istio.test.variant", settingsFromCommandLine.Image.Variant,
		"Common Container variant to use when deploying container images")
	flag.StringVar(&settingsFromCommandLine.Image.PullPolicy, "istio.test.pullpolicy", settingsFromCommandLine.Image.PullPolicy,
		"Common image pull policy to use when deploying container images")
	flag.StringVar(&settingsFromCommandLine.Image.PullSecret, "istio.test.imagePullSecret", settingsFromCommandLine.Image.PullSecret,
		"Path to a file containing a DockerConfig secret use for test apps. This will be pushed to all created namespaces."+
			"Secret should already exist when used with istio.test.stableNamespaces.")
	flag.Uint64Var(&settingsFromCommandLine.MaxDumps, "istio.test.maxDumps", settingsFromCommandLine.MaxDumps,
		"Maximum number of full test dumps that are allowed to occur within a test suite.")
	flag.StringVar(&settingsFromCommandLine.HelmRepo, "istio.test.helmRepo", settingsFromCommandLine.HelmRepo, "Helm repo to use to pull the charts.")
	flag.Var(&settingsFromCommandLine.IPFamilies, "istio.test.IPFamilies",
		"IP families (IPv6, IPv4) to test with. If both specified, dualstack config will be used. The order the families are defined indicates precedence.")
	flag.BoolVar(&settingsFromCommandLine.GatewayConformanceStandardOnly, "istio.test.gatewayConformanceStandardOnly",
		settingsFromCommandLine.GatewayConformanceStandardOnly,
		"If set, only the standard gateway conformance tests will be run; tests relying on experimental resources will be skipped.")

	flag.BoolVar(&settingsFromCommandLine.OpenShift, "istio.test.openshift", settingsFromCommandLine.OpenShift,
		"Indicate the tests run in an OpenShift platform rather than in plain Kubernetes.")

	flag.BoolVar(&settingsFromCommandLine.AmbientMultiNetwork, "istio.test.ambient.multinetwork", settingsFromCommandLine.AmbientMultiNetwork,
		"Indicate the use of ambient multicluster.")

	initGatewayConformanceTimeouts()
}

func initGatewayConformanceTimeouts() {
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.CreateTimeout, "istio.test.gatewayConformance.createTimeout",
		0, "Gateway conformance test timeout for waiting for creating a k8s resource.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.DeleteTimeout, "istio.test.gatewayConformance.deleteTimeout",
		0, "Gateway conformance test timeout for getting a k8s resource.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.GetTimeout, "istio.test.gatewayConformance.geTimeout",
		0, "Gateway conformance test timeout for getting a k8s resource.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.GatewayMustHaveAddress,
		"istio.test.gatewayConformance.gatewayMustHaveAddressTimeout", 0,
		"Gateway conformance test timeout for waiting for a Gateway to have an address.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.GatewayMustHaveCondition,
		"istio.test.gatewayConformance.gatewayMustHaveConditionTimeout", 0,
		"Gateway conformance test timeout for waiting for a Gateway to have a certain condition.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.GatewayStatusMustHaveListeners,
		"istio.test.gatewayConformance.gatewayStatusMustHaveListenersTimeout", 0,
		"Gateway conformance test timeout for waiting for a Gateway's status to have listeners.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.GatewayListenersMustHaveConditions,
		"istio.test.gatewayConformance.gatewayListenersMustHaveConditionTimeout", 0,
		"Gateway conformance test timeout for waiting for a Gateway's listeners to have certain conditions.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.GWCMustBeAccepted,
		"istio.test.gatewayConformance.gatewayClassMustBeAcceptedTimeout",
		0, "Gateway conformance test timeout for waiting for a GatewayClass to be accepted.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.HTTPRouteMustNotHaveParents,
		"istio.test.gatewayConformance.httpRouteMustNotHaveParentsTimeout", 0,
		"Gateway conformance test timeout for waiting for an HTTPRoute to either have no parents or a single parent that is not accepted.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.HTTPRouteMustHaveCondition,
		"istio.test.gatewayConformance.httpRouteMustHaveConditionTimeout",
		0, "Gateway conformance test timeout for waiting for an HTTPRoute to have a certain condition.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.TLSRouteMustHaveCondition,
		"istio.test.gatewayConformance.tlsRouteMustHaveConditionTimeout", 0,
		"Gateway conformance test timeout for waiting for an TLSRoute to have a certain condition.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.RouteMustHaveParents, "istio.test.gatewayConformance.routeMustHaveParentsTimeout",
		0, "Gateway conformance test timeout for the the maximum time for an xRoute to have parents in status that match the expected parents.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.ManifestFetchTimeout, "istio.test.gatewayConformance.manifestFetchTimeout",
		0, "Gateway conformance test timeout for the maximum time for getting content from a https:// URL.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.MaxTimeToConsistency, "istio.test.gatewayConformance.maxTimeToConsistency",
		0, "Gateway conformance test setting for the maximum time for requiredConsecutiveSuccesses (default 3) requests to succeed in a row before failing the test.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.NamespacesMustBeReady,
		"istio.test.gatewayConformance.namespacesMustBeReadyTimeout", 0,
		"Gateway conformance test timeout for waiting for namespaces to be ready.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.RequestTimeout, "istio.test.gatewayConformance.requestTimeout",
		0, "Gateway conformance test timeout for an HTTP request.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.LatestObservedGenerationSet,
		"istio.test.gatewayConformance.latestObservedGenerationSetTimeout", 0,
		"Gateway conformance test timeout for waiting for a latest observed generation to be set.")
	flag.DurationVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.DefaultTestTimeout, "istio.test.gatewayConformance.defaultTestTimeout",
		0, "Default gateway conformance test case timeout.")
	flag.IntVar(&settingsFromCommandLine.GatewayConformanceTimeoutConfig.RequiredConsecutiveSuccesses,
		"istio.test.gatewayConformance.requiredConsecutiveSuccesses", 0,
		"Gateway conformance test setting for the required number of consecutive successes before failing the test.")
}
