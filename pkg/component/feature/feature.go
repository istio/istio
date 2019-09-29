// Copyright 2017 Istio Authors
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

package feature

import (
	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/component/component"
	"istio.io/operator/pkg/name"
	"istio.io/operator/pkg/translate"
	"istio.io/operator/pkg/util"
)

// IstioFeature is a feature corresponding to Istio features defined in the IstioControlPlane proto.
type IstioFeature interface {
	// Run starts the Istio feature operation. Must be called before feature can be used.
	Run() error
	// RenderManifest returns a manifest string rendered against the IstioControlPlane parameters.
	RenderManifest() (name.ManifestMap, util.Errors)
}

// Options are options for IstioFeature.
type Options struct {
	// InstallSpec is the installation spec for the control plane.
	InstallSpec *v1alpha2.IstioControlPlaneSpec
	// Translator is the translator for this feature.
	Translator *translate.Translator
}

// CommonFeatureFields are fields common to all features.
type CommonFeatureFields struct {
	// Options is an embedded struct.
	Options
	// components is a slice of components that are part of the feature.
	components []component.IstioComponent
}

func NewFeature(ft name.FeatureName, opts *Options) IstioFeature {
	var feature IstioFeature
	switch ft {
	case name.IstioBaseFeatureName:
		feature = NewBaseFeature(opts)
	case name.TrafficManagementFeatureName:
		feature = NewTrafficManagementFeature(opts)
	case name.PolicyFeatureName:
		feature = NewPolicyFeature(opts)
	case name.TelemetryFeatureName:
		feature = NewTelemetryFeature(opts)
	case name.SecurityFeatureName:
		feature = NewSecurityFeature(opts)
	case name.ConfigManagementFeatureName:
		feature = NewConfigManagementFeature(opts)
	case name.AutoInjectionFeatureName:
		feature = NewAutoInjectionFeature(opts)
	case name.GatewayFeatureName:
		feature = NewGatewayFeature(opts)
	case name.CNIFeatureName:
		feature = NewCNIFeature(opts)
	case name.ThirdPartyFeatureName:
		feature = NewThirdPartyFeature(opts)
	}
	return feature
}

// BaseFeature is the base feature, containing essential Istio base items.
type BaseFeature struct {
	// CommonFeatureFields is the struct shared among all features.
	CommonFeatureFields
}

// NewBaseFeature creates a new BaseFeature and returns a pointer to it.
func NewBaseFeature(opts *Options) *BaseFeature {
	cff := buildCommonFeatureFields(opts, name.IstioBaseFeatureName)
	return &BaseFeature{
		CommonFeatureFields: *cff,
	}
}

// Run implements the IstioFeature interface.
func (f *BaseFeature) Run() error {
	return runComponents(f.components)
}

// RenderManifest implements the IstioFeature interface.
func (f *BaseFeature) RenderManifest() (name.ManifestMap, util.Errors) {
	return renderComponents(f.components)
}

// TrafficManagementFeature is the traffic management feature.
type TrafficManagementFeature struct {
	// CommonFeatureFields is the struct shared among all features.
	CommonFeatureFields
}

// NewTrafficManagementFeature creates a new TrafficManagementFeature and returns a pointer to it.
func NewTrafficManagementFeature(opts *Options) *TrafficManagementFeature {
	cff := buildCommonFeatureFields(opts, name.TrafficManagementFeatureName)
	return &TrafficManagementFeature{
		CommonFeatureFields: *cff,
	}
}

// Run implements the IstioFeature interface.
func (f *TrafficManagementFeature) Run() error {
	return runComponents(f.components)
}

// RenderManifest implements the IstioFeature interface.
func (f *TrafficManagementFeature) RenderManifest() (name.ManifestMap, util.Errors) {
	return renderComponents(f.components)
}

// SecurityFeature is the security feature.
type SecurityFeature struct {
	CommonFeatureFields
}

// NewSecurityFeature creates a new SecurityFeature and returns a pointer to it.
func NewSecurityFeature(opts *Options) *SecurityFeature {
	cff := buildCommonFeatureFields(opts, name.SecurityFeatureName)
	return &SecurityFeature{
		CommonFeatureFields: *cff,
	}
}

// Run implements the IstioFeature interface.
func (f *SecurityFeature) Run() error {
	return runComponents(f.components)
}

// RenderManifest implements the IstioFeature interface.
func (f *SecurityFeature) RenderManifest() (name.ManifestMap, util.Errors) {
	return renderComponents(f.components)
}

// PolicyFeature is the policy feature.
type PolicyFeature struct {
	CommonFeatureFields
}

// NewPolicyFeature creates a new PolicyFeature and returns a pointer to it.
func NewPolicyFeature(opts *Options) *PolicyFeature {
	cff := buildCommonFeatureFields(opts, name.PolicyFeatureName)
	return &PolicyFeature{
		CommonFeatureFields: *cff,
	}
}

// Run implements the IstioFeature interface.
func (f *PolicyFeature) Run() error {
	return runComponents(f.components)
}

// RenderManifest implements the IstioFeature interface.
func (f *PolicyFeature) RenderManifest() (name.ManifestMap, util.Errors) {
	return renderComponents(f.components)
}

// TelemetryFeature is the telemetry feature.
type TelemetryFeature struct {
	CommonFeatureFields
}

// Run implements the IstioFeature interface.
func (f *TelemetryFeature) Run() error {
	return runComponents(f.components)
}

// NewTelemetryFeature creates a new TelemetryFeature and returns a pointer to it.
func NewTelemetryFeature(opts *Options) *TelemetryFeature {
	cff := buildCommonFeatureFields(opts, name.TelemetryFeatureName)
	return &TelemetryFeature{
		CommonFeatureFields: *cff,
	}
}

// RenderManifest implements the IstioFeature interface.
func (f *TelemetryFeature) RenderManifest() (name.ManifestMap, util.Errors) {
	return renderComponents(f.components)
}

// ConfigManagementFeature is the config management feature.
type ConfigManagementFeature struct {
	CommonFeatureFields
}

// NewConfigManagementFeature creates a new ConfigManagementFeature and returns a pointer to it.
func NewConfigManagementFeature(opts *Options) *ConfigManagementFeature {
	cff := buildCommonFeatureFields(opts, name.ConfigManagementFeatureName)
	return &ConfigManagementFeature{
		CommonFeatureFields: *cff,
	}
}

// Run implements the IstioFeature interface.
func (f *ConfigManagementFeature) Run() error {
	return runComponents(f.components)
}

// RenderManifest implements the IstioFeature interface.
func (f *ConfigManagementFeature) RenderManifest() (name.ManifestMap, util.Errors) {
	return renderComponents(f.components)
}

// AutoInjectionFeature is the auto injection feature.
type AutoInjectionFeature struct {
	CommonFeatureFields
}

// NewAutoInjectionFeature creates a new AutoInjectionFeature and returns a pointer to it.
func NewAutoInjectionFeature(opts *Options) *AutoInjectionFeature {
	cff := buildCommonFeatureFields(opts, name.AutoInjectionFeatureName)
	return &AutoInjectionFeature{
		CommonFeatureFields: *cff,
	}
}

// Run implements the IstioFeature interface.
func (f *AutoInjectionFeature) Run() error {
	return runComponents(f.components)
}

// RenderManifest implements the IstioFeature interface.
func (f *AutoInjectionFeature) RenderManifest() (name.ManifestMap, util.Errors) {
	return renderComponents(f.components)
}

// GatewayFeature is the istio gateways feature.
type GatewayFeature struct {
	CommonFeatureFields
}

// NewGatewayFeature creates a new GatewayFeature and returns a pointer to it.
func NewGatewayFeature(opts *Options) *GatewayFeature {
	cff := buildCommonFeatureFields(opts, name.GatewayFeatureName)
	return &GatewayFeature{
		CommonFeatureFields: *cff,
	}
}

// Run implements the IstioFeature interface.
func (f *GatewayFeature) Run() error {
	return runComponents(f.components)
}

// RenderManifest implements the IstioFeature interface.
func (f *GatewayFeature) RenderManifest() (name.ManifestMap, util.Errors) {
	return renderComponents(f.components)
}

// ThirdPartyFeature is the third party feature.
type ThirdPartyFeature struct {
	// CommonFeatureFields is the struct shared among all features.
	CommonFeatureFields
}

// NewThirdPartyFeature creates a new ThirdPartyFeature and returns a pointer to it.
func NewThirdPartyFeature(opts *Options) *ThirdPartyFeature {
	cff := buildCommonFeatureFields(opts, name.ThirdPartyFeatureName)
	return &ThirdPartyFeature{
		CommonFeatureFields: *cff,
	}
}

// Run implements the IstioFeature interface.
func (f *ThirdPartyFeature) Run() error {
	return runComponents(f.components)
}

// RenderManifest implements the IstioFeature interface.
func (f *ThirdPartyFeature) RenderManifest() (name.ManifestMap, util.Errors) {
	return renderComponents(f.components)
}

// CNIFeature is the cni feature.
type CNIFeature struct {
	// CommonFeatureFields is the struct shared among all features.
	CommonFeatureFields
}

// NewCNIFeature creates a new CNIFeature and returns a pointer to it.
func NewCNIFeature(opts *Options) *CNIFeature {
	cff := buildCommonFeatureFields(opts, name.CNIFeatureName)
	return &CNIFeature{
		CommonFeatureFields: *cff,
	}
}

// Run implements the IstioFeature interface.
func (f *CNIFeature) Run() error {
	return runComponents(f.components)
}

// RenderManifest implements the IstioFeature interface.
func (f *CNIFeature) RenderManifest() (name.ManifestMap, util.Errors) {
	return renderComponents(f.components)
}

// newComponentOptions creates a component.ComponentOptions ptr from the given parameters.
func newComponentOptions(cff *CommonFeatureFields, featureName name.FeatureName) *component.Options {
	return &component.Options{
		InstallSpec: cff.InstallSpec,
		FeatureName: featureName,
		Translator:  cff.Translator,
	}
}

// runComponents calls Run on all components in a feature.
func runComponents(cs []component.IstioComponent) error {
	for _, c := range cs {
		if err := c.Run(); err != nil {
			return err
		}
	}
	return nil
}

// renderComponents calls render manifest for all components in a feature and concatenates the outputs.
func renderComponents(cs []component.IstioComponent) (manifests name.ManifestMap, errsOut util.Errors) {
	manifests = make(name.ManifestMap)
	for _, c := range cs {
		m, err := c.RenderManifest()
		errsOut = util.AppendErr(errsOut, err)
		manifests[c.Name()] = m
	}
	if len(errsOut) > 0 {
		return nil, errsOut
	}
	return
}

// buildCommonFeatureFields is an internal function to build the Common Feature Fields for specified feature.
func buildCommonFeatureFields(opts *Options, ftname name.FeatureName) *CommonFeatureFields {
	cff := &CommonFeatureFields{
		Options: *opts,
	}
	if opts == nil || opts.Translator == nil || opts.Translator.FeatureMaps == nil {
		return cff
	}
	ftMap := opts.Translator.FeatureMaps[ftname]
	for _, cn := range ftMap.Components {
		enabled, err := opts.Translator.IsComponentEnabled(cn, opts.InstallSpec)
		if err == nil && enabled {
			cff.components = append(cff.components, component.NewComponent(cn, newComponentOptions(cff, ftname)))
		}
	}
	return cff
}
