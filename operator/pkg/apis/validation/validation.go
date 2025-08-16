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

package validation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/apis"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/values"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/validation/agent"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/util/protomarshal"
)

type Warnings = util.Errors

func ParseAndValidateIstioOperator(iopm values.Map, client kube.Client) (Warnings, util.Errors) {
	iop := &apis.IstioOperator{}
	dec := json.NewDecoder(bytes.NewBufferString(iopm.JSON()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(iop); err != nil {
		return nil, util.NewErrs(fmt.Errorf("could not unmarshal: %v", err))
	}

	var warnings Warnings
	var errors util.Errors

	if client != nil {
		vw, ve := environmentalDetection(client, iop)
		warnings = util.AppendErrs(warnings, vw)
		errors = util.AppendErrs(errors, ve)
	}

	vw, ve := validateValues(iop)
	warnings = util.AppendErrs(warnings, vw)
	errors = util.AppendErrs(errors, ve)

	mw, me := validateMeshConfig(string(iop.Spec.MeshConfig))
	warnings = util.AppendErrs(warnings, mw)
	errors = util.AppendErrs(errors, me)

	errors = util.AppendErr(errors, validateHub(iop.Spec.Hub))
	errors = util.AppendErr(errors, validateTag(iop.Spec.Tag))
	errors = util.AppendErr(errors, validateRevision(iop.Spec.Revision))
	errors = util.AppendErr(errors, validateComponentNames(iop.Spec.Components))

	return warnings, errors
}

// nolint: unparam
func environmentalDetection(client kube.Client, iop *apis.IstioOperator) (Warnings, util.Errors) {
	var warnings Warnings
	var errors util.Errors
	cniEnabled := iop.Spec.Components != nil && iop.Spec.Components.Cni != nil && iop.Spec.Components.Cni.Enabled.GetValueOrFalse()
	ztunnelEnabled := iop.Spec.Components != nil && iop.Spec.Components.Ztunnel != nil && iop.Spec.Components.Ztunnel.Enabled.GetValueOrFalse()
	warnings = util.AppendErrs(warnings, detectCniIncompatibility(client, cniEnabled, ztunnelEnabled))
	return warnings, errors
}

func detectCniIncompatibility(client kube.Client, cniEnabled bool, ztunnelEnabled bool) util.Errors {
	var errs util.Errors
	calicoResource := schema.GroupVersionResource{Group: "projectcalico.org", Version: "v3", Resource: "felixconfigurations"}
	calico, err := client.Dynamic().Resource(calicoResource).Get(context.Background(), "default", metav1.GetOptions{})
	if err == nil {
		bpfConnectTimeLoadBalancing, found, _ := unstructured.NestedString(calico.Object, "spec", "bpfConnectTimeLoadBalancing")
		if found && bpfConnectTimeLoadBalancing != "Disabled" {
			// Need to disable Calico connect-time load balancing since it send traffic directly to the backend pod IP
			// nolint: lll
			errs = util.AppendErr(errs, fmt.Errorf("detected Calico CNI with 'bpfConnectTimeLoadBalancing=%s'; this must be set to 'bpfConnectTimeLoadBalancing=Disabled' in the Calico configuration", bpfConnectTimeLoadBalancing))
		}
		bpfConnectTimeLoadBalancingEnabled, found, _ := unstructured.NestedBool(calico.Object, "spec", "bpfConnectTimeLoadBalancingEnabled")
		if found && bpfConnectTimeLoadBalancingEnabled {
			// Same behavior as BpfconnectTimeLoadBalancing
			// nolint: lll
			errs = util.AppendErr(errs, fmt.Errorf("detected Calico CNI with 'bpfConnectTimeLoadBalancingEnabled=true'; this must be set to 'bpfConnectTimeLoadBalancingEnabled=false' in the Calico configuration"))
		}
	}
	cilium, err := client.Kube().CoreV1().ConfigMaps("kube-system").Get(context.Background(), "cilium-config", metav1.GetOptions{})
	if err == nil {
		if cniEnabled && cilium.Data["cni-exclusive"] == "true" {
			// Without this, Cilium will constantly overwrite our CNI config.
			errs = util.AppendErr(errs,
				fmt.Errorf("detected Cilium CNI with 'cni-exclusive=true'; this must be set to 'cni-exclusive=false' in the Cilium configuration"))
		}
		if ztunnelEnabled && cilium.Data["enable-bpf-masquerade"] == "true" {
			// See https://github.com/istio/istio/issues/52208
			errs = util.AppendErr(errs,
				fmt.Errorf("detected Cilium CNI with 'enable-bpf-masquerade=true'; this must be set to 'false' when using ambient mode"))
		}
		bpfLbSocket := cilium.Data["bpf-lb-sock"] == "true"                 // Unset implies 'false', so this check is ok
		bpfLbHostnsOnly := cilium.Data["bpf-lb-sock-hostns-only"] == "true" // Unset implies 'false', so this check is ok
		if bpfLbSocket && !bpfLbHostnsOnly {
			// See https://github.com/istio/istio/issues/27619
			errs = util.AppendErr(errs,
				errors.New("detected Cilium CNI with 'bpf-lb-sock=true'; this requires 'bpf-lb-sock-hostns-only=true' to be set"))
		}
		// Cilium version differences:
		// * Older versions of Cilium (<v0.16) used "kube-proxy-replacement=strict" and
		//   defaulted to "bpf-lb-sock-hostns-only=false". This could cause compatibility
		//   issues with Istio, as traffic might bypass the sidecar proxy.
		//
		// * Newer versions of Cilium (>=v0.16) no longer support "strict". Instead, they
		//   use "kube-proxy-replacement=true/false", where the default behavior is
		//   "bpf-lb-sock-hostns-only=true".
		kpr := cilium.Data["kube-proxy-replacement"]
		if kpr == "strict" {
			if !bpfLbHostnsOnly {
				errs = util.AppendErr(errs,
					errors.New("detected Cilium CNI with 'kube-proxy-replacement=strict' and 'bpf-lb-sock-hostns-only=false'; "+
						"please set 'bpf-lb-sock-hostns-only=true' to avoid conflicts with Istio"))
			}
		}
	}
	return errs
}

func validateValues(raw *apis.IstioOperator) (Warnings, util.Errors) {
	values := &apis.Values{}
	if err := yaml.Unmarshal(raw.Spec.Values, values); err != nil {
		return nil, util.NewErrs(fmt.Errorf("could not unmarshal: %v", err))
	}
	warnings, errs := validateFeatures(values, raw.Spec)
	if values.MeshConfig != nil {
		j, err := protomarshal.ToJSON(values.MeshConfig)
		if err != nil {
			errs = util.AppendErr(errs, err)
		} else {
			warn, err := validateMeshConfig(j)
			warnings = util.AppendErrs(warnings, warn)
			errs = util.AppendErrs(errs, err)
		}
	}

	run := func(v any, f validatorFunc, p string) {
		if !reflect.ValueOf(v).IsZero() {
			errs = util.AppendErrs(errs, f(util.PathFromString(p), v))
		}
	}

	run(values.GetGlobal().GetProxy().GetIncludeIPRanges(), validateIPRangesOrStar, "global.proxy.includeIPRanges")
	run(values.GetGlobal().GetProxy().GetExcludeIPRanges(), validateIPRangesOrStar, "global.proxy.excludeIPRanges")
	run(values.GetGlobal().GetProxy().GetIncludeInboundPorts(), validateStringList(validatePortNumberString), "global.proxy.includeInboundPorts")
	run(values.GetGlobal().GetProxy().GetExcludeInboundPorts(), validateStringList(validatePortNumberString), "global.proxy.excludeInboundPorts")

	return warnings, errs
}

func validateMeshConfig(contents string) (Warnings, util.Errors) {
	mc, err := mesh.ApplyMeshConfigDefaults(contents)
	if err != nil {
		return nil, util.NewErrs(err)
	}
	warnings, errors := agent.ValidateMeshConfig(mc)
	if errors != nil {
		return nil, util.NewErrs(err)
	}
	if warnings != nil {
		return util.NewErrs(warnings), nil
	}
	return nil, nil
}

type FeatureValidator func(*apis.Values, apis.IstioOperatorSpec) (Warnings, util.Errors)

func validateNativeNftablesWithDistroless(values *apis.Values) Warnings {
	var warnings Warnings

	// Check if nativeNftables is enabled
	nativeNftablesEnabled := false
	if values.GetGlobal() != nil && values.GetGlobal().GetNativeNftables() != nil && values.GetGlobal().GetNativeNftables().GetValue() {
		nativeNftablesEnabled = true
	}

	// Check if image is distroless
	isDistroless := false
	if values.GetGlobal() != nil && values.GetGlobal().GetVariant() == "distroless" {
		isDistroless = true
	}

	// If nativeNftables is enabled and image is distroless, add a warning
	if nativeNftablesEnabled && isDistroless {
		warnings = util.AppendErr(warnings, fmt.Errorf("nativeNftables is enabled, but the image is distroless."+
			" The 'nft' CLI binary is not available in distroless images, which is required for nativeNftables to work."+
			" Please either disable nativeNftables or use a non-distroless image"))
	}

	return warnings
}

// validateFeatures check whether the config semantically make sense. For example, feature X and feature Y can't be enabled together.
func validateFeatures(values *apis.Values, spec apis.IstioOperatorSpec) (Warnings, util.Errors) {
	var warnings Warnings
	var errors util.Errors

	// Check nativeNftables with distroless images
	// Revisit: Once the PR https://github.com/istio/istio/pull/56917 is merged.
	w := validateNativeNftablesWithDistroless(values)
	warnings = util.AppendErrs(warnings, w)

	validators := []FeatureValidator{
		checkServicePorts,
		checkAutoScaleAndReplicaCount,
	}

	for _, validator := range validators {
		newWarnings, newErrs := validator(values, spec)
		warnings = util.AppendErrs(warnings, newWarnings)
		errors = util.AppendErrs(errors, newErrs)
	}

	return warnings, errors
}

// checkAutoScaleAndReplicaCount warns when autoscaleEnabled is true and k8s replicaCount is set.
func checkAutoScaleAndReplicaCount(values *apis.Values, spec apis.IstioOperatorSpec) (Warnings, util.Errors) {
	if spec.Components == nil {
		return nil, nil
	}
	var warnings Warnings
	if values.GetPilot().GetAutoscaleEnabled().GetValue() {
		if spec.Components.Pilot != nil && spec.Components.Pilot.Kubernetes != nil && spec.Components.Pilot.Kubernetes.ReplicaCount > 1 {
			warnings = append(warnings,
				fmt.Errorf("components.pilot.k8s.replicaCount should not be set when values.pilot.autoscaleEnabled is true"))
		}
	}

	validateGateways := func(gateways []apis.GatewayComponentSpec, gwType string) {
		const format = "components.%sGateways[name=%s].k8s.replicaCount should not be set when values.gateways.istio-%sgateway.autoscaleEnabled is true"
		for _, gw := range gateways {
			if gw.Kubernetes != nil && gw.Kubernetes.ReplicaCount != 0 {
				warnings = append(warnings, fmt.Errorf(format, gwType, gw.Name, gwType))
			}
		}
	}

	if values.GetGateways().GetIstioIngressgateway().GetAutoscaleEnabled().GetValue() {
		validateGateways(spec.Components.IngressGateways, "ingress")
	}

	if values.GetGateways().GetIstioEgressgateway().GetAutoscaleEnabled().GetValue() {
		validateGateways(spec.Components.EgressGateways, "egress")
	}

	return warnings, nil
}

// checkServicePorts validates Service ports. Specifically, this currently
// asserts that all ports will bind to a port number greater than 1024 when not
// running as root.
func checkServicePorts(values *apis.Values, spec apis.IstioOperatorSpec) (Warnings, util.Errors) {
	var errs util.Errors
	if spec.Components != nil {
		if !values.GetGateways().GetIstioIngressgateway().GetRunAsRoot().GetValue() {
			errs = util.AppendErrs(errs, validateGateways(spec.Components.IngressGateways, "istio-ingressgateway"))
		}
		if !values.GetGateways().GetIstioEgressgateway().GetRunAsRoot().GetValue() {
			errs = util.AppendErrs(errs, validateGateways(spec.Components.EgressGateways, "istio-egressgateway"))
		}
	}
	for _, raw := range values.GetGateways().GetIstioIngressgateway().GetIngressPorts() {
		p := raw.AsMap()
		var tp int
		if p["targetPort"] != nil {
			t, ok := p["targetPort"].(float64)
			if !ok {
				continue
			}
			tp = int(t)
		}

		rport, ok := p["port"].(float64)
		if !ok {
			continue
		}
		portnum := int(rport)
		if tp == 0 && portnum > 1024 {
			// Target port defaults to port. If its >1024, it is safe.
			continue
		}
		if tp < 1024 {
			// nolint: lll
			errs = util.AppendErr(errs, fmt.Errorf("port %v is invalid: targetPort is set to %v, which requires root. Set targetPort to be greater than 1024 or configure values.gateways.istio-ingressgateway.runAsRoot=true", portnum, tp))
		}
	}
	return nil, errs
}

func validateGateways(gws []apis.GatewayComponentSpec, name string) util.Errors {
	// nolint: lll
	format := "port %v/%v in gateway %v invalid: targetPort is set to %d, which requires root. Set targetPort to be greater than 1024 or configure values.gateways.%s.runAsRoot=true"
	var errs util.Errors
	for _, gw := range gws {
		if gw.Kubernetes == nil || gw.Kubernetes.Service == nil {
			continue
		}
		for _, p := range gw.Kubernetes.Service.Ports {
			tp := 0
			if p.TargetPort.Type == intstr.String {
				// Do not validate named ports
				continue
			}
			if p.TargetPort.Type == intstr.Int {
				tp = int(p.TargetPort.IntVal)
			}
			if tp == 0 && p.Port > 1024 {
				// Target port defaults to port. If its >1024, it is safe.
				continue
			}
			if tp < 1024 {
				errs = util.AppendErr(errs, fmt.Errorf(format, p.Name, p.Port, gw.Name, tp, name))
			}
		}
	}
	return errs
}

func validateHub(hub string) error {
	if hub == "" {
		return nil
	}
	// Takes a <hub>/image, we have <hub>, so pass an image we would use
	_, err := name.NewRepository(hub + "/pilot")
	if err != nil {
		return fmt.Errorf("bad hub: %v", err)
	}
	return nil
}

func validateTag(tag any) error {
	if tag == nil {
		return nil
	}
	// Takes a <hub>/<image>:<tag>, we just have <tag>
	_, err := name.NewTag(fmt.Sprintf("istio/pilot:%v", tag))
	if err != nil {
		return fmt.Errorf("bad tag: %v", err)
	}
	return nil
}

func validateRevision(revision string) error {
	if revision == "" {
		return nil
	}
	if !labels.IsDNS1123Label(revision) {
		err := fmt.Errorf("invalid revision specified: %s", revision)
		return util.Errors{err}
	}
	return nil
}

// ObjectNameRegexp is a legal name for a k8s object.
var objectNameRegexp = regexp.MustCompile(`[a-z0-9.-]{1,254}`)

func validateComponentNames(components *apis.IstioComponentSpec) error {
	if components == nil {
		return nil
	}
	for _, gw := range components.EgressGateways {
		if gw.Name == "" {
			continue
		}
		if err := validateWithRegex(objectNameRegexp, "egressGateways.name", gw.Name); err != nil {
			return err
		}
	}
	for _, gw := range components.IngressGateways {
		if gw.Name == "" {
			continue
		}
		if err := validateWithRegex(objectNameRegexp, "inressGateways.name", gw.Name); err != nil {
			return err
		}
	}
	return nil
}

func validateWithRegex(r *regexp.Regexp, context string, val string) (err error) {
	if len(r.FindString(val)) != len(val) {
		return fmt.Errorf("invalid value %s: %v", context, val)
	}
	return nil
}

// validatorFunc validates a value.
type validatorFunc func(path util.Path, i any) util.Errors

// validateStringList returns a validator function that works on a string list, using the supplied validatorFunc vf on
// each element.
func validateStringList(vf validatorFunc) validatorFunc {
	return func(path util.Path, val any) util.Errors {
		if !util.IsString(val) {
			return util.NewErrs(fmt.Errorf("validateStringList %s got %T, want string", path, val))
		}
		var errs util.Errors
		for _, s := range strings.Split(val.(string), ",") {
			errs = util.AppendErrs(errs, vf(path, s))
		}
		return errs
	}
}

// validatePortNumberString checks if val is a string with a valid port number.
func validatePortNumberString(path util.Path, val any) util.Errors {
	if !util.IsString(val) {
		return util.NewErrs(fmt.Errorf("validatePortNumberString(%s) bad type %T, want string", path, val))
	}
	if val.(string) == "*" || val.(string) == "" {
		return nil
	}
	intV, err := strconv.ParseInt(val.(string), 10, 32)
	if err != nil {
		return util.NewErrs(fmt.Errorf("%s : %s", path, err))
	}
	return validatePortNumber(path, intV)
}

// validatePortNumber checks whether val is an integer representing a valid port number.
func validatePortNumber(path util.Path, val any) util.Errors {
	return validateIntRange(path, val, 0, 65535)
}

// validateIPRangesOrStar validates IP ranges and also allow star, examples: "1.1.0.256/16,2.2.0.257/16", "*"
func validateIPRangesOrStar(path util.Path, val any) (errs util.Errors) {
	if !util.IsString(val) {
		err := fmt.Errorf("validateIPRangesOrStar %s got %T, want string", path, val)
		return util.NewErrs(err)
	}

	if val.(string) == "*" || val.(string) == "" {
		return errs
	}

	return validateStringList(validateCIDR)(path, val)
}

// validateIntRange checks whether val is an integer in [min, max].
func validateIntRange(path util.Path, val any, minimum, maximum int64) util.Errors {
	k := reflect.TypeOf(val).Kind()
	var err error
	switch {
	case util.IsIntKind(k):
		v := reflect.ValueOf(val).Int()
		if v < minimum || v > maximum {
			err = fmt.Errorf("value %s:%v falls outside range [%v, %v]", path, v, minimum, maximum)
		}
	case util.IsUintKind(k):
		v := reflect.ValueOf(val).Uint()
		if int64(v) < minimum || int64(v) > maximum {
			err = fmt.Errorf("value %s:%v falls out side range [%v, %v]", path, v, minimum, maximum)
		}
	default:
		err = fmt.Errorf("validateIntRange %s unexpected type %T, want int type", path, val)
	}
	return util.NewErrs(err)
}

// validateCIDR checks whether val is a string with a valid CIDR.
func validateCIDR(path util.Path, val any) util.Errors {
	var err error
	if !util.IsString(val) {
		err = fmt.Errorf("validateCIDR %s got %T, want string", path, val)
	} else {
		if _, err = netip.ParsePrefix(val.(string)); err != nil {
			err = fmt.Errorf("%s %s", path, err)
		}
	}
	return util.NewErrs(err)
}
