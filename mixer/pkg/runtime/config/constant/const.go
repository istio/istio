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

package constant

// TODO merge all the mixer wide constants in this file.
const (
	// DefaultConfigNamespace holds istio wide configuration.
	DefaultConfigNamespace = "istio-system"

	// RulesKind defines the config kind Name of mixer Rules.
	RulesKind = "rule"

	// AdapterKind defines the config kind Name of adapter infos (name, templates is consumes, and its own configuration).
	AdapterKind = "adapter"

	// TemplateKind defines the config kind Name of mixer templates.
	TemplateKind = "template"

	// HandlerKind defines the config kind Name of mixer handlers
	HandlerKind = "handler"

	// InstanceKind defines the config kind Name of mixer instances
	InstanceKind = "instance"

	// AttributeManifestKind define the config kind Name of attribute manifests.
	AttributeManifestKind = "attributemanifest"
)
