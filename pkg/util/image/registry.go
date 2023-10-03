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
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// Exists returns true if the image in the argument exists in a container registry.
// The argument must be a complete image name, e.g. "gcr.io/istio-release/pilot:1.20.0".
// If the image does not exist, it returns false and an optional error message, for debug purposes.
func Exists(image string) (bool, error) {
	ref, err := name.ParseReference(image)
	if err != nil {
		return false, fmt.Errorf("parsing reference %q: %w", image, err)
	}
	_, err = remote.Get(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err == nil {
		// image exists
		return true, nil
	}
	isUnknown := strings.Contains(err.Error(), string(transport.ManifestUnknownErrorCode))
	if isUnknown {
		// image does not exist
		return false, nil
	}
	// Some other error
	return false, err
}
