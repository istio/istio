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

package auth

import (
	"fmt"

	"istio.io/api/authentication/v1alpha1"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"

	v1 "k8s.io/api/core/v1"
	k8s_labels "k8s.io/apimachinery/pkg/labels"

	"istio.io/istio/galley/pkg/config/analysis/msg"

	meshconfig "istio.io/api/mesh/v1alpha1"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/auth/mtls"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

const missingResourceName = "(none)"

// MTLSAnalyzer checks for misconfigurations of MTLS policy when autoMtls is
// disabled. More specifically, it detects situations where a DestinationRule's
// MTLS usage is in conflict with mTLS specified by a policy.
//
// The most common situations that this detects are: 1. A MeshPolicy exists that
// requires mTLS, but no global destination rule
//    says to use mTLS.
// 2. mTLS is used throughout the mesh, but a DestinationRule is added that
//    doesn't specify mTLS (usually because it was forgotten).
//
// The analyzer tries to act more generally by imagining service-to-service
// traffic and detecting whether or not there's a conflict with regards to mTLS
// policy. This means it will also detect explicit misconfigurations as well.
//
// Note this is very similar to `istioctl authn tls-check`; however this
// inspection is all done via analyzing configuration rather than requiring a
// connection to pilot.
type MTLSAnalyzer struct{}

// Compile-time check that this Analyzer correctly implements the interface
var _ analysis.Analyzer = &MTLSAnalyzer{}

// Metadata implements Analyzer
func (s *MTLSAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "auth.MTLSAnalyzer",
		Description: "Checks for misconfigurations of MTLS policy when autoMtls is disabled",
		// Each analyzer should register the collections that it needs to use as input.
		Inputs: collection.Names{
			metadata.K8SCoreV1Pods,
			metadata.K8SCoreV1Namespaces,
			metadata.K8SCoreV1Services,
			metadata.IstioAuthenticationV1Alpha1Meshpolicies,
			metadata.IstioAuthenticationV1Alpha1Policies,
			metadata.IstioMeshV1Alpha1MeshConfig,
			metadata.IstioNetworkingV1Alpha3Destinationrules,
		},
	}
}

// Analyze implements Analyzer
func (s *MTLSAnalyzer) Analyze(c analysis.Context) {
	// TODO Reuse pilot logic as a library rather than reproducing its logic
	// here.

	// If autoMTLS is turned on, bail out early as the logic used below does not
	// reason about its usage.
	autoMtlsEnabled := false
	rootNamespace := "istio-system"

	// Only one MeshConfig should exist in practice - we use ForEach to avoid
	// specifying where to look for the MeshConfig (which allows the Context
	// object to provide it to us).
	c.ForEach(metadata.IstioMeshV1Alpha1MeshConfig, func(r *resource.Entry) bool {
		mc := r.Item.(*meshconfig.MeshConfig)
		if mc.GetEnableAutoMtls() != nil && mc.GetEnableAutoMtls().Value {
			autoMtlsEnabled = true
		}

		if mc.GetRootNamespace() != "" {
			rootNamespace = mc.GetRootNamespace()
		}
		return true
	})

	if autoMtlsEnabled {
		return
	}

	// Loop over all services, building up a list of selectors for each. This is
	// used to determine which pods are in which services, and determine whether
	// or not the sidecar is fully enmeshed. If a service doesn't have a
	// sidecar, then we always treat it as having an explicit "plaintext" policy
	// regardless of the service/namespace/mesh policy.

	var targetServices []mtls.TargetService
	fqdnsWithoutSidecars := make(map[string]struct{})
	// Keep track of each fqdn -> port name -> port number. This is because
	// the Policy object lets you target a port name, but DR requires port
	// number. Tracking this means we can normalize to port number later.
	fqdnToNameToPort := make(map[string]map[string]uint32)

	c.ForEach(metadata.K8SCoreV1Services, func(r *resource.Entry) bool {
		svcNs, svcName := r.Metadata.Name.InterpretAsNamespaceAndName()

		// Skip the istio control plane. It doesn't obey Policy/MeshPolicy MTLS
		// rules in general and instead is controlled by the mesh option
		// 'controlPlaneSecurityEnabled'.
		if svcNs == "istio-system" {
			return true
		}
		svc := r.Item.(*v1.ServiceSpec)

		svcSelector := k8s_labels.SelectorFromSet(svc.Selector)
		fqdn := util.ConvertHostToFQDN(svcNs, svcName)
		for _, port := range svc.Ports {
			portNumber := uint32(port.Port)
			// portName is optional, but we note it so we can translate later.
			if port.Name != "" {
				// allocate a new map if necessary
				if _, ok := fqdnToNameToPort[fqdn]; !ok {
					fqdnToNameToPort[fqdn] = make(map[string]uint32)
				}
				fqdnToNameToPort[fqdn][port.Name] = portNumber
			}

			targetServices = append(targetServices, mtls.NewTargetServiceWithPortNumber(fqdn, portNumber))
		}

		// Now we loop over all pods looking for sidecars that match our
		// service. If we find a single pod without a sidecar, we label the
		// service as not having a sidecar (which means we bypass policy
		// checking). If we find no pods at all that match, also assume there's
		// no sidecar.
		var foundMatchingPods bool
		c.ForEach(metadata.K8SCoreV1Pods, func(pr *resource.Entry) bool {
			// If it's not in our namespace, we're not interested
			podNs, _ := pr.Metadata.Name.InterpretAsNamespaceAndName()
			if podNs != svcNs {
				return true
			}
			pod := pr.Item.(*v1.Pod)
			podLabels := k8s_labels.Set(pod.ObjectMeta.Labels)

			if svcSelector.Empty() || !svcSelector.Matches(podLabels) {
				return true
			}

			// This pod is selected for this service - ensure there's a sidecar.
			foundMatchingPods = true
			sidecarFound := false
			for _, container := range pod.Spec.Containers {
				if container.Name == "istio-proxy" {
					sidecarFound = true
				}
			}

			if !sidecarFound {
				fqdnsWithoutSidecars[fqdn] = struct{}{}
			}
			return true
		})

		if !foundMatchingPods {
			fqdnsWithoutSidecars[fqdn] = struct{}{}
		}

		return true
	})

	// While we visit every item, collect the set of namespaces that exist. Note
	// that we will collect the namespace name for all resource types - this
	// ensures our analyzer still behaves correctly even if namespaces are
	// implicitly defined.
	namespaces := make(map[string]struct{})

	c.ForEach(metadata.K8SCoreV1Namespaces, func(r *resource.Entry) bool {
		_, name := r.Metadata.Name.InterpretAsNamespaceAndName()
		namespaces[name] = struct{}{}
		return true
	})

	pc := mtls.NewPolicyChecker(fqdnToNameToPort)
	meshPolicyResource := c.Find(metadata.IstioAuthenticationV1Alpha1Meshpolicies, resource.NewName("", "default"))
	if meshPolicyResource != nil {
		err := pc.AddMeshPolicy(meshPolicyResource, meshPolicyResource.Item.(*v1alpha1.Policy))
		if err != nil {
			c.Report(metadata.IstioAuthenticationV1Alpha1Meshpolicies, msg.NewInternalError(meshPolicyResource, err.Error()))
			return
		}
	}

	c.ForEach(metadata.IstioAuthenticationV1Alpha1Policies, func(r *resource.Entry) bool {
		ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()
		namespaces[ns] = struct{}{}

		err := pc.AddPolicy(r, r.Item.(*v1alpha1.Policy))
		if err != nil {
			// AddPolicy can return a NamedPortInPolicyNotFoundError - if it
			// does we can print a useful message.
			// TODO this should be in its own analyzer, and ignored here.
			if missingPortNameErr, ok := err.(mtls.NamedPortInPolicyNotFoundError); ok {
				c.Report(metadata.IstioAuthenticationV1Alpha1Meshpolicies,
					msg.NewPolicySpecifiesPortNameThatDoesntExist(r, missingPortNameErr.PortName, missingPortNameErr.FQDN))
				return true
			}
			c.Report(metadata.IstioAuthenticationV1Alpha1Meshpolicies, msg.NewInternalError(r, err.Error()))
			return false
		}
		return true
	})

	drc := mtls.NewDestinationRuleChecker(rootNamespace)
	c.ForEach(metadata.IstioNetworkingV1Alpha3Destinationrules, func(r *resource.Entry) bool {
		ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()
		namespaces[ns] = struct{}{}

		drc.AddDestinationRule(r, r.Item.(*v1alpha3.DestinationRule))
		return true
	})

	// Here we explicitly handle the common case where a user specifies a
	// MeshPolicy with no global DestinationRule. We also track if we report a
	// problem with the global configuration. This is used later to suppress
	// reporting a message for every service/namespace combination due to the
	// same misconfiguration.
	anyK8sServiceHost := fmt.Sprintf("%s.%s", util.Wildcard, util.DefaultKubernetesDomain)
	globalMTLSMisconfigured := false
	mpr := pc.MeshPolicy()
	globalMtls, globalDR := drc.DoesNamespaceUseMTLSToService(rootNamespace, rootNamespace, mtls.NewTargetService(anyK8sServiceHost))
	if mpr.MTLSMode == mtls.ModeStrict && !globalMtls {
		// We may or may not have a matching DR. If we don't, use the special
		// missing resource string
		globalDRName := missingResourceName
		if globalDR != nil {
			globalDRName = globalDR.Metadata.Name.String()
		}
		c.Report(
			metadata.IstioAuthenticationV1Alpha1Meshpolicies,
			msg.NewMTLSPolicyConflict(
				mpr.Resource,
				anyK8sServiceHost,
				globalDRName,
				globalMtls,
				mpr.Resource.Metadata.Name.String(),
				mpr.MTLSMode.String()))
		globalMTLSMisconfigured = true
	}

	// Also handle the less-common case where a global DR exists that specifies
	// mtls, but MTLS is off
	if mpr.MTLSMode == mtls.ModePlaintext && globalMtls {
		// We may or may not have a matching policy. If we don't, use the
		// special missing resource string
		globalPolicyName := missingResourceName
		if mpr.Resource != nil {
			globalPolicyName = mpr.Resource.Metadata.Name.String()
		}
		c.Report(
			metadata.IstioNetworkingV1Alpha3Destinationrules,
			msg.NewMTLSPolicyConflict(
				globalDR,
				anyK8sServiceHost,
				globalDR.Metadata.Name.String(),
				globalMtls,
				globalPolicyName,
				mpr.MTLSMode.String()))
		globalMTLSMisconfigured = true
	}

	// Iterate over all fqdns and namespaces, and check that the mtls mode
	// specified by the destination rule and the policy are not in conflict.
	for _, ts := range targetServices {
		var tsPolicy mtls.ModeAndResource
		// If we don't have a sidecar, don't check policy and treat as plaintext
		if _, ok := fqdnsWithoutSidecars[ts.FQDN()]; ok {
			tsPolicy = mtls.ModeAndResource{
				MTLSMode: mtls.ModePlaintext,
				Resource: nil,
			}
		} else {
			var err error
			tsPolicy, err = pc.IsServiceMTLSEnforced(ts)
			if err != nil {
				c.Report(metadata.IstioAuthenticationV1Alpha1Policies, msg.NewInternalError(nil, err.Error()))
				return
			}
		}

		// Extract out the namespace for the target service
		tsNamespace, _ := util.GetNamespaceAndNameFromFQDN(ts.FQDN())

		for ns := range namespaces {
			mtlsUsed, matchingDR := drc.DoesNamespaceUseMTLSToService(ns, tsNamespace, ts)
			if (tsPolicy.MTLSMode == mtls.ModeStrict && !mtlsUsed) ||
				(tsPolicy.MTLSMode == mtls.ModePlaintext && mtlsUsed) {

				// If global mTLS is misconfigured, and one of the resources we
				// are about to complain about is missing, it's almost certainly
				// due to the same underlying problem (a missing global
				// DR/MeshPolicy). In that case, don't emit since it's redundant.
				if globalMTLSMisconfigured && (tsPolicy.Resource == nil || matchingDR == nil) {
					continue
				}

				// Check to see if our mismatch is due to a missing sidecar. If
				// so, use a different analyzer message.
				if _, ok := fqdnsWithoutSidecars[ts.FQDN()]; ok {
					c.Report(metadata.IstioNetworkingV1Alpha3Destinationrules,
						msg.NewDestinationRuleUsesMTLSForWorkloadWithoutSidecar(
							matchingDR,
							matchingDR.Metadata.Name.String(),
							ts.String()))
					continue
				}

				if tsPolicy.Resource != nil {
					// We may or may not have a matching DR. If we don't, use
					// the special missing resource string
					matchingDRName := missingResourceName
					if matchingDR != nil {
						matchingDRName = matchingDR.Metadata.Name.String()
					}
					c.Report(
						metadata.IstioAuthenticationV1Alpha1Policies,
						msg.NewMTLSPolicyConflict(
							tsPolicy.Resource,
							ts.String(),
							matchingDRName,
							mtlsUsed,
							tsPolicy.Resource.Metadata.Name.String(),
							tsPolicy.MTLSMode.String()))
				}
				if matchingDR != nil {
					// We may or may not have a matching policy. If we don't, use
					// the special missing resource string
					policyName := missingResourceName
					if tsPolicy.Resource != nil {
						policyName = tsPolicy.Resource.Metadata.Name.String()
					}
					c.Report(
						metadata.IstioNetworkingV1Alpha3Destinationrules,
						msg.NewMTLSPolicyConflict(
							matchingDR,
							ts.String(),
							matchingDR.Metadata.Name.String(),
							mtlsUsed,
							policyName,
							tsPolicy.MTLSMode.String()))
				}
			}
		}
	}
}
