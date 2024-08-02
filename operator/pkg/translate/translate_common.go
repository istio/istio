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

package translate

import (
	"fmt"

	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/util"
)

// OverlayValuesEnablement overlays any enablement in values path from the user file overlay or set flag overlay.
// The overlay is translated from values to the corresponding addonComponents enablement paths.
func OverlayValuesEnablement(baseYAML, fileOverlayYAML, setOverlayYAML string) (string, error) {
	overlayYAML, err := util.OverlayYAML(fileOverlayYAML, setOverlayYAML)
	if err != nil {
		return "", fmt.Errorf("could not overlay user config over base: %s", err)
	}

	return YAMLTree(overlayYAML, baseYAML, name.ValuesEnablementPathMap)
}
