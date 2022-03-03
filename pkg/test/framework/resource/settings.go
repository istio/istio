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
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/label"
)

const (
	// maxTestIDLength is the maximum length allowed for testID.
	maxTestIDLength = 30
)

// ImageSettings for container images.
type ImageSettings struct {
	// Hub value to use in Helm templates
	Hub string

	// Tag value to use in Helm templates
	Tag string

	// Image pull policy to use for deployments. If not specified, the defaults of each deployment will be used.
	PullPolicy string

	// PullSecret path to a file containing a k8s secret in yaml so test pods can pull from protected registries.
	PullSecret string
}

func (s *ImageSettings) PullSecretName() (string, error) {
	if s.PullSecret == "" {
		return "", nil
	}
	data, err := os.ReadFile(s.PullSecret)
	if err != nil {
		return "", err
	}
	secret := unstructured.Unstructured{Object: map[string]interface{}{}}
	if err := yaml.Unmarshal(data, secret.Object); err != nil {
		return "", err
	}
	return secret.GetName(), nil
}

func (s *ImageSettings) PullSecretNameOrFail(t test.Failer) string {
	t.Helper()
	out, err := s.PullSecretName()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Settings is the set of arguments to the test driver.
type Settings struct {
	// Name of the test
	TestID string

	RunID uuid.UUID

	// Do not cleanup the resources after the test run.
	NoCleanup bool

	// Indicates that the tests are running in CI Mode
	CIMode bool

	// Should the tests fail if usage of deprecated stuff (e.g. Envoy flags) is detected
	FailOnDeprecation bool

	// Local working directory root for creating temporary directories / files in. If left empty,
	// os.TempDir() will be used.
	BaseDir string

	// The number of times to retry failed tests.
	// This should not be depended on as a primary means for reducing test flakes.
	Retries int

	// If enabled, namespaces will be reused rather than created with dynamic names each time.
	// This is useful when combined with NoCleanup, to allow quickly iterating on tests.
	StableNamespaces bool

	// The label selector that the user has specified.
	SelectorString string

	// The regex specifying which tests to skip. This follows inverted semantics of golang's
	// -test.run flag, which only supports positive match. If an entire package is meant to be
	// excluded, it can be filtered with `go list` and explicitly passing the list of desired
	// packages. For example: `go test $(go list ./... | grep -v bad-package)`.
	SkipString  ArrayFlags
	SkipMatcher *Matcher

	// SkipWorkloadClasses can be used to skip deploying special workload types like TPROXY, VMs, etc.
	SkipWorkloadClasses ArrayFlags

	// The label selector, in parsed form.
	Selector label.Selector

	// EnvironmentFactory allows caller to override the environment creation. If nil, a default is used based
	// on the known environment names.
	EnvironmentFactory EnvironmentFactory

	// Deprecated: prefer to use `--istio.test.revisions=<revision name>`.
	// The revision label on a namespace for injection webhook.
	// If set to XXX, all the namespaces created with istio-injection=enabled will be replaced with istio.io/rev=XXX.
	Revision string

	// Skip VM related parts for all the tests.
	SkipVM bool

	// Skip Delta XDS related parts for all the tests.
	SkipDelta bool

	// Skip TProxy related parts for all the tests.
	SkipTProxy bool

	// Compatibility determines whether we should transparently deploy echo workloads attached to each revision
	// specified in `Revisions` when creating echo instances. Used primarily for compatibility testing between revisions
	// on different control plane versions.
	Compatibility bool

	// Revisions maps the Istio revisions that are available to each cluster to their corresponding versions.
	// This flag must be used with --istio.test.kube.deploy=false with the versions pre-installed.
	// This flag should be passed in as comma-separated values, such as "rev-a=1.7.3,rev-b=1.8.2,rev-c=1.9.0", and the test framework will
	// spin up pods pointing to these revisions for each echo instance and skip tests accordingly.
	// To configure it so that an Istio revision is on the latest version simply list the revision name without the version (i.e. "rev-a,rev-b")
	// If using this flag with --istio.test.revision, this flag will take precedence.
	Revisions RevVerMap

	// Image settings
	Image ImageSettings
}

func (s Settings) Skip(class string) bool {
	return s.SkipWorkloadClassesAsSet().Contains(class)
}

func (s *Settings) SkipWorkloadClassesAsSet() sets.Set {
	return sets.NewSet(s.SkipWorkloadClasses...)
}

// RunDir is the name of the dir to output, for this particular run.
func (s *Settings) RunDir() string {
	u := strings.Replace(s.RunID.String(), "-", "", -1)
	t := strings.Replace(s.TestID, "_", "-", -1)
	// We want at least 6 characters of uuid padding
	padding := maxTestIDLength - len(t)
	if padding < 0 {
		padding = 0
	}
	n := fmt.Sprintf("%s-%s", t, u[0:padding])

	return path.Join(s.BaseDir, n)
}

// Clone settings
func (s *Settings) Clone() *Settings {
	cl := *s
	return &cl
}

// DefaultSettings returns a default settings instance.
func DefaultSettings() *Settings {
	return &Settings{
		RunID: uuid.New(),
	}
}

// String implements fmt.Stringer
func (s *Settings) String() string {
	result := ""

	result += fmt.Sprintf("TestID:            %s\n", s.TestID)
	result += fmt.Sprintf("RunID:             %s\n", s.RunID.String())
	result += fmt.Sprintf("NoCleanup:         %v\n", s.NoCleanup)
	result += fmt.Sprintf("BaseDir:           %s\n", s.BaseDir)
	result += fmt.Sprintf("Selector:          %v\n", s.Selector)
	result += fmt.Sprintf("FailOnDeprecation: %v\n", s.FailOnDeprecation)
	result += fmt.Sprintf("CIMode:            %v\n", s.CIMode)
	result += fmt.Sprintf("Retries:           %v\n", s.Retries)
	result += fmt.Sprintf("StableNamespaces:  %v\n", s.StableNamespaces)
	result += fmt.Sprintf("Revision:          %v\n", s.Revision)
	result += fmt.Sprintf("SkipWorkloads      %v\n", s.SkipWorkloadClasses)
	result += fmt.Sprintf("Compatibility:     %v\n", s.Compatibility)
	result += fmt.Sprintf("Revisions:         %v\n", s.Revisions.String())
	result += fmt.Sprintf("Hub:               %s\n", s.Image.Hub)
	result += fmt.Sprintf("Tag:               %s\n", s.Image.Tag)
	result += fmt.Sprintf("PullPolicy:        %s\n", s.Image.PullPolicy)
	result += fmt.Sprintf("PullSecret:        %s\n", s.Image.PullSecret)
	return result
}

type ArrayFlags []string

func (i *ArrayFlags) String() string {
	return fmt.Sprint([]string(*i))
}

func (i *ArrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}
