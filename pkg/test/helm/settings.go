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

package helm

import kubeCore "k8s.io/api/core/v1"

// Settings for Helm based deployment. These get passed directly to Helm.
type Settings struct {
	Tag             string
	Hub             string
	ImagePullPolicy kubeCore.PullPolicy

	EnableCoreDump bool

	GlobalMtlsEnabled bool
	GalleyEnabled     bool
}

// generate a map[string]string for easy processing.
func (s *Settings) generate() map[string]string {
	// TODO: Add more flags, as needed.
	return map[string]string{
		"global.tag":                  s.Tag,
		"global.hub":                  s.Hub,
		"global.imagePullPolicy":      string(s.ImagePullPolicy),
		"global.proxy.enableCoreDump": boolString(s.EnableCoreDump),
		"global.mtls.enabled":         boolString(s.GlobalMtlsEnabled),
		"galley.enabled":              boolString(s.GalleyEnabled),
	}
}

// DefaultSettings returns a default set of settings.
func DefaultSettings() *Settings {
	return &Settings{
		ImagePullPolicy:   kubeCore.PullIfNotPresent,
		GlobalMtlsEnabled: true,
		GalleyEnabled:     true,
	}
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
