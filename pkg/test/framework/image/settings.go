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

package image

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"istio.io/istio/pkg/test"
)

const (
	// HubValuesKey values key for the Docker image hub.
	HubValuesKey = "global.hub"

	// TagValuesKey values key for the Docker image tag.
	TagValuesKey = "global.tag"

	// ImagePullPolicyValuesKey values key for the Docker image pull policy.
	ImagePullPolicyValuesKey = "global.imagePullPolicy"

	// LatestTag value
	LatestTag = "latest"
)

// Settings provide kube-specific Settings from flags.
type Settings struct {
	// Hub value to use in Helm templates
	Hub string

	// Tag value to use in Helm templates
	Tag string

	// Image pull policy to use for deployments. If not specified, the defaults of each deployment will be used.
	PullPolicy string

	// ImagePullSecret path to a file containing a k8s secret in yaml so test pods can pull from protected registries.
	ImagePullSecret string
}

func (s *Settings) clone() *Settings {
	c := *s
	return &c
}

// String implements fmt.Stringer
func (s *Settings) String() string {
	result := ""

	result += fmt.Sprintf("Hub:             %s\n", s.Hub)
	result += fmt.Sprintf("Tag:             %s\n", s.Tag)
	result += fmt.Sprintf("PullPolicy:      %s\n", s.PullPolicy)
	result += fmt.Sprintf("ImagePullSecret: %s\n", s.ImagePullSecret)

	return result
}

func (s *Settings) ImagePullSecretName() (string, error) {
	if s.ImagePullSecret == "" {
		return "", nil
	}
	data, err := os.ReadFile(s.ImagePullSecret)
	if err != nil {
		return "", err
	}
	secret := unstructured.Unstructured{Object: map[string]interface{}{}}
	if err := yaml.Unmarshal(data, secret.Object); err != nil {
		return "", err
	}
	return secret.GetName(), nil
}

func PullSecretNameOrFail(t test.Failer) string {
	s, err := SettingsFromCommandLine()
	if err != nil {
		t.Fatalf("failed reading image settings: %v", err)
	}
	name, err := s.ImagePullSecretName()
	if err != nil {
		t.Fatalf("failed getting name of image pull secret: %v", err)
	}
	return name
}

func PullImagePolicy(t test.Failer) string {
	var (
		Always       = "Always"
		IfNotPresent = "IfNotPresent"
		Never        = "Never"
	)

	s, err := SettingsFromCommandLine()
	if err != nil || s == nil {
		t.Logf("failed reading image settings: %v, set imagePullPolicy=Always", err)
		return Always
	}
	switch s.PullPolicy {
	case Always, "":
		return Always
	case IfNotPresent:
		return IfNotPresent
	case Never:
		return Never
	default:
		t.Logf("invalid image pull policy: %s, set imagePullPolicy=Always", s.PullPolicy)
		return Always
	}
}
