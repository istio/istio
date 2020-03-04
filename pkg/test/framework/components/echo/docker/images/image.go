// Copyright 2019 Istio Authors
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

package images

import (
	"fmt"

	"istio.io/istio/pkg/test/framework/core/image"
)

const (
	noSidecarImageName = "app"
	sidecarImageName   = "app_sidecar"
)

// Instance for the Echo application
type Instance struct {
	// NoSidecar is the Docker image for the plain Echo application without a sidecar.
	NoSidecar string

	// Sidecar is the Docker image for the Echo application bundled with the sidecar (pilot-agent, envoy, ip-tables, etc.)
	Sidecar string
}

// Get the images for the Echo application.
func Get() (Instance, error) {
	s, err := image.SettingsFromCommandLine()
	if err != nil {
		return Instance{}, err
	}

	prebuilt := Instance{
		NoSidecar: fmt.Sprintf("%s/%s:%s", s.Hub, noSidecarImageName, s.Tag),
		Sidecar:   fmt.Sprintf("%s/%s:%s", s.Hub, sidecarImageName, s.Tag),
	}

	return prebuilt, nil
}
