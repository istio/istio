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

package istio

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

var versionRegex = regexp.MustCompile(`^([1-9]+)\.([0-9]+)(\.([0-9]+))?`)

// detectIstioVersionFromPods attempts to detect the Istio version from running pods
func detectIstioVersionFromPods(ctx resource.Context) (resource.RevVerMap, error) {
	revisions := make(resource.RevVerMap)

	for _, cluster := range ctx.Clusters() {
		// Try to get istiod pods first
		pods, err := cluster.Kube().CoreV1().Pods("istio-system").List(
			context.TODO(), metav1.ListOptions{LabelSelector: "app=istiod"})
		if err != nil {
			scopes.Framework.Debugf("Failed to get istiod pods from cluster %s: %v", cluster.Name(), err)
			continue
		}

		if len(pods.Items) == 0 {
			scopes.Framework.Debugf("No istiod pods found in cluster %s", cluster.Name())
			continue
		}

		pod := pods.Items[0]

		// Try multiple label patterns for version detection
		versionLabels := []string{
			"istio.io/version",                    // Direct version label
			"version",                             // Short version label
			"istio",                               // Sometimes contains version info
			"service.istio.io/canonical-revision", // Revision-based version
			"app.kubernetes.io/version",           // Openshift based version
		}

		var detectedVersion string
		revisionName := "default"

		// Check labels for version info
		for _, labelKey := range versionLabels {
			if labelValue, exists := pod.Labels[labelKey]; exists && labelValue != "" {
				if version := extractVersionFromString(labelValue); version != "" {
					detectedVersion = version
					scopes.Framework.Infof("Detected Istio version %s from label %s=%s", version, labelKey, labelValue)
					break
				}
			}
		}

		// Fallback: check revision label to determine revision name
		if revLabel, exists := pod.Labels["istio.io/rev"]; exists && revLabel != "" {
			revisionName = revLabel
		}

		// Fallback: extract version from container image if labels don't work
		if detectedVersion == "" {
			for _, container := range pod.Spec.Containers {
				if container.Name == "discovery" {
					if version := extractVersionFromImage(container.Image); version != "" {
						detectedVersion = version
						scopes.Framework.Infof("Detected Istio version %s from container image %s", version, container.Image)
						break
					}
				}
			}
		}

		// Store the detected version
		if detectedVersion != "" {
			revisions[revisionName] = resource.IstioVersion(detectedVersion)
			scopes.Framework.Infof("Successfully detected Istio revision '%s' with version '%s'", revisionName, detectedVersion)
		}
	}

	if len(revisions) == 0 {
		return nil, fmt.Errorf("could not detect Istio version from any cluster")
	}

	return revisions, nil
}

// extractVersionFromString extracts a semantic version from a string
func extractVersionFromString(s string) string {
	// Remove common prefixes and suffixes
	s = strings.TrimSpace(s)

	// Match semantic version patterns
	match := versionRegex.FindStringSubmatch(s)
	if len(match) > 1 {
		return match[0] // Return the captured version number
	}

	return ""
}

// extractVersionFromImage extracts version from container image tag
func extractVersionFromImage(image string) string {
	// Handle images like: gcr.io/istio-testing/pilot:1.28.0, pilot:1.28.0-latest
	parts := strings.Split(image, ":")
	if len(parts) < 2 {
		return ""
	}

	tag := parts[len(parts)-1]

	// Remove common suffixes like -latest, -alpha, -beta
	for _, suffix := range []string{"-latest", "-alpha", "-beta", "-rc1", "-rc2"} {
		tag = strings.TrimSuffix(tag, suffix)
	}

	return extractVersionFromString(tag)
}
