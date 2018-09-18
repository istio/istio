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

package deploy

// Settings for Helm based deployment. These get passed directly to Helm
type settings struct {
	Tag string
	Hub string

	EnableCoreDump bool

	GlobalMtlsEnabled bool
	GalleyEnabled     bool
}

func (s *settings) generateHelmSettings() map[string]string {
	return map[string]string{
		"global.tag":                  s.Tag,
		"global.hub":                  s.Hub,
		"global.proxy.enableCoreDump": boolString(s.EnableCoreDump),
		"global.mtls.enabled":         boolString(s.GlobalMtlsEnabled),
		"galley.enabled":              boolString(s.GalleyEnabled),
	}
}

func defaultIstioSettings() settings {
	return settings{
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
