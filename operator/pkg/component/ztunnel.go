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

package component

import (
	"istio.io/istio/operator/pkg/name"
)

// ZtunnelComponent is the istio ztunnel component.
type ZtunnelComponent struct {
	*CommonComponentFields
}

// NewZtunnelComponent creates a new ZtunnelComponent and returns a pointer to it.
func NewZtunnelComponent(opts *Options) *ZtunnelComponent {
	return &ZtunnelComponent{
		&CommonComponentFields{
			Options:       opts,
			ComponentName: name.ZtunnelComponentName,
		},
	}
}

// Run implements the IstioComponent interface.
func (c *ZtunnelComponent) Run() error {
	return runComponent(c.CommonComponentFields)
}

// RenderManifest implements the IstioComponent interface.
func (c *ZtunnelComponent) RenderManifest() (string, error) {
	return renderManifest(c, c.CommonComponentFields)
}

// ComponentName implements the IstioComponent interface.
func (c *ZtunnelComponent) ComponentName() name.ComponentName {
	return c.CommonComponentFields.ComponentName
}

// ResourceName implements the IstioComponent interface.
func (c *ZtunnelComponent) ResourceName() string {
	return c.CommonComponentFields.ResourceName
}

// Namespace implements the IstioComponent interface.
func (c *ZtunnelComponent) Namespace() string {
	return c.CommonComponentFields.Namespace
}

// Enabled implements the IstioComponent interface.
func (c *ZtunnelComponent) Enabled() bool {
	return isCoreComponentEnabled(c.CommonComponentFields)
}
