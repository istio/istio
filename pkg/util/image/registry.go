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
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Exists returns true if the image in the argument exists in a container registry.
// The argument must be a complete image name, e.g. "gcr.io/istio-release/pilot:1.19.0".
// If the image does not exist, it returns false and an optional error message, for debug purposes.
func Exists(image string) (bool, error) {
	c := exec.Command("crane", "manifest", image)
	b := &bytes.Buffer{}
	c.Stderr = b
	err := c.Run()
	if err == nil {
		return true, nil
	}

	if strings.Contains(b.String(), "MANIFEST_UNKNOWN") {
		return false, fmt.Errorf(b.String())
	}

	return false, fmt.Errorf("failed to check image existence: %v, %v", err, b.String())
}
