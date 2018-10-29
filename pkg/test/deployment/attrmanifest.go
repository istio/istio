//  Copyright 2018 Istio Authors
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

package deployment

import (
	"errors"
	"strings"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/scopes"
)

// ExtractAttributeManifest extracts attribute manifest from Helm charts.
func ExtractAttributeManifest() (string, error) {
	// We don't care about deploymentName, namespace or values file, or other settings, as we only
	// want to extract attribute manifest, which is not really templatized.
	s, err := HelmTemplate(
		"attributemanifest",
		"istio-system",
		env.IstioChartDir,
		"", nil)
	if err != nil {
		return "", err
	}

	// Split the template into chunks
	parts := test.SplitConfigs(s)
	for _, part := range parts {
		// Yaml contains both the CR and the CRD. We only want CR.
		if strings.Contains(part, "kind: attributemanifest") &&
			!strings.Contains(part, "kind: CustomResourceDefinition") {

			scopes.Framework.Debugf("Extracted AttributeManifest:\n%s\n", part)
			return part, nil
		}
	}

	return "", errors.New("attribute manifest not found in generated chart")
}
