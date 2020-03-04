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

package mtls

import (
	"fmt"

	"istio.io/api/authentication/v1alpha1"

	"istio.io/istio/pkg/config/resource"

	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
)

// TargetService is a simple struct type for representing a service
// targeted by an Authentication policy.
type TargetService struct {
	fqdn       string
	portNumber uint32
}

// NewTargetServiceWithPortNumber creates a new TargetService using the specified
// fqdn and portNumber.
func NewTargetServiceWithPortNumber(fqdn string, portNumber uint32) TargetService {
	return TargetService{fqdn: fqdn, portNumber: portNumber}
}

// NewTargetService creates a new TargetService using the specified fqdn. Because no
// port is specified, this implicitly represents the service bound to any port.
func NewTargetService(fqdn string) TargetService {
	return TargetService{fqdn: fqdn}
}

// FQDN is the fully-qualified domain name for the service (e.g.
// foobar.my-namespace.svc.cluster.local).
func (w TargetService) FQDN() string {
	return w.fqdn
}

// PortNumber is the port used by the service.
func (w TargetService) PortNumber() uint32 {
	return w.portNumber
}

func (w TargetService) String() string {
	if w.PortNumber() != 0 {
		return fmt.Sprintf("%s:%d", w.fqdn, w.portNumber)
	}
	return w.fqdn
}

// PolicyChecker allows callers to add a set of v1alpha1.Policy objects in the
// mesh. Once these are loaded, you can query whether or not a specific
// TargetService will require MTLS when an incoming connection occurs using the
// IsServiceMTLSEnforced() call.
type PolicyChecker struct {
	meshMTLSModeAndResource ModeAndResource

	namespaceToMTLSMode        map[resource.Namespace]ModeAndResource
	serviceToMTLSMode          map[TargetService]ModeAndResource
	fqdnToPortNameToPortNumber map[string]map[string]uint32
}

// Mode is a special type used to distinguish between MTLS being off
// (unsupported), permissive (supported but not required), and strict (required).
type Mode int

const (
	// ModePlaintext means MTLS is off (unsupported).
	ModePlaintext Mode = iota
	// ModePermissive means MTLS is permissive (supported but not required)
	ModePermissive
	// ModeStrict means MTLS is strict (required)
	ModeStrict
)

func (m Mode) String() string {
	switch m {
	case ModePlaintext:
		return "Plaintext"
	case ModePermissive:
		return "Permissive"
	case ModeStrict:
		return "Strict"
	default:
		return "UNKNOWN"
	}
}

// ModeAndResource is a simple tuple type of mode and the resource that
// specified the mode.
type ModeAndResource struct {
	MTLSMode Mode
	Resource *resource.Instance
}

// NewPolicyChecker creates a new PolicyChecker instance.
func NewPolicyChecker(fqdnToPortNameToPortNumber map[string]map[string]uint32) *PolicyChecker {
	return &PolicyChecker{
		namespaceToMTLSMode:        make(map[resource.Namespace]ModeAndResource),
		serviceToMTLSMode:          make(map[TargetService]ModeAndResource),
		fqdnToPortNameToPortNumber: fqdnToPortNameToPortNumber,
	}
}

// AddMeshPolicy adds a mesh-level policy to the checker. Note that there can
// only be at most one mesh level policy in effect.
func (pc *PolicyChecker) AddMeshPolicy(r *resource.Instance, p *v1alpha1.Policy) error {
	mode, err := parsePolicyMTLSMode(p)
	if err != nil {
		return err
	}
	pc.meshMTLSModeAndResource = ModeAndResource{Resource: r, MTLSMode: mode}
	return nil
}

// MeshPolicy returns the current recognized MeshPolicy (as added by AddMeshPolicy).
func (pc *PolicyChecker) MeshPolicy() ModeAndResource {
	return pc.meshMTLSModeAndResource
}

type NamedPortInPolicyNotFoundError struct {
	PortName     string
	PolicyOrigin string
	FQDN         string
}

func (e NamedPortInPolicyNotFoundError) Error() string {
	return fmt.Sprintf("named port '%s' not found for fqdn '%s', unable to analyze policy '%s'", e.PortName, e.FQDN, e.PolicyOrigin)
}

// AddPolicy adds a new policy object to the PolicyChecker to use when later
// determining if a service is MTLS-enforced. The namespace of the policy is
// also provided as some policies can target the local namespace.
//
// If the Policy uses a named port, and the port cannot be looked up in the map
// provided to NewPolicyChecker, then an error of type
// NamedPortInPolicyNotFoundError is returned.
func (pc *PolicyChecker) AddPolicy(r *resource.Instance, p *v1alpha1.Policy) error {
	mode, err := parsePolicyMTLSMode(p)
	if err != nil {
		return err
	}
	modeAndResource := ModeAndResource{Resource: r, MTLSMode: mode}
	namespace := r.Metadata.FullName.Namespace
	if len(p.Targets) == 0 {
		// Rule targets the namespace.
		pc.namespaceToMTLSMode[namespace] = modeAndResource
		return nil
	}
	// Discover the targeted service and take note. Should normalize.
	for _, target := range p.Targets {
		fqdn := util.ConvertHostToFQDN(namespace, target.Name)

		if len(target.Ports) == 0 {
			// Policy targets all ports on service
			pc.serviceToMTLSMode[NewTargetService(fqdn)] = modeAndResource
		}

		for _, port := range target.Ports {
			if port.GetName() != "" {
				// Look up the port number for the name. If we can't find it, we
				// need to complain about a different error
				// TODO handle missing reference error.
				if _, ok := pc.fqdnToPortNameToPortNumber[fqdn]; !ok {
					return NamedPortInPolicyNotFoundError{
						PortName:     port.GetName(),
						PolicyOrigin: r.Origin.FriendlyName(),
						FQDN:         fqdn,
					}
				}
				portNumber := pc.fqdnToPortNameToPortNumber[fqdn][port.GetName()]
				if portNumber == 0 {
					return NamedPortInPolicyNotFoundError{
						PortName:     port.GetName(),
						PolicyOrigin: r.Origin.FriendlyName(),
						FQDN:         fqdn,
					}
				}

				pc.serviceToMTLSMode[NewTargetServiceWithPortNumber(fqdn, portNumber)] = modeAndResource
			} else if port.GetNumber() != 0 {
				pc.serviceToMTLSMode[NewTargetServiceWithPortNumber(fqdn, port.GetNumber())] = modeAndResource
			} else {
				// Unhandled case!
				return fmt.Errorf("policy has a port with no name/number for target %s", target.Name)
			}
		}
	}

	return nil
}

// IsServiceMTLSEnforced returns true if a service requires incoming
// connections to use MTLS, or false if MTLS is not a hard-requirement (e.g.
// mode is permissive, peerIsOptional is true, etc). Only call this after adding
// all policy resources in effect via AddPolicy or AddMeshPolicy.
func (pc *PolicyChecker) IsServiceMTLSEnforced(w TargetService) (ModeAndResource, error) {
	// TODO support understanding port name -> port number mappings
	var modeAndResource ModeAndResource
	modeAndResource = pc.serviceToMTLSMode[w]
	if modeAndResource.Resource != nil {
		return modeAndResource, nil
	}

	// Try checking if its enforced on any ports
	serviceNoPort := NewTargetService(w.FQDN())
	modeAndResource = pc.serviceToMTLSMode[serviceNoPort]
	if modeAndResource.Resource != nil {
		return modeAndResource, nil
	}

	// Check if enforced on namespace
	namespace := util.GetResourceNameFromHost("", w.FQDN()).Namespace
	if namespace == "" {
		return ModeAndResource{}, fmt.Errorf("unable to extract namespace from fqdn: %s", w.FQDN())
	}
	modeAndResource = pc.namespaceToMTLSMode[namespace]
	if modeAndResource.Resource != nil {
		return modeAndResource, nil
	}

	// Finally, defer to mesh level policy. No need to check for a nil resource
	// since the default value is correct.
	return pc.meshMTLSModeAndResource, nil
}

// TargetServices returns the list of services known to the policy checker (in
// no particular order).
func (pc *PolicyChecker) TargetServices() []TargetService {
	tss := make([]TargetService, len(pc.serviceToMTLSMode))
	i := 0
	for ts := range pc.serviceToMTLSMode {
		tss[i] = ts
		i++
	}

	return tss
}

// parsePolicyMTLSMode is a helper function to determine what mtls mode a Policy
// implies.
func parsePolicyMTLSMode(p *v1alpha1.Policy) (Mode, error) {
	for _, peer := range p.Peers {
		mtlsParams, ok := peer.Params.(*v1alpha1.PeerAuthenticationMethod_Mtls)
		if !ok {
			// Only looking for mtls methods
			continue
		}

		var mode Mode
		if mtlsParams.Mtls == nil {
			mode = ModeStrict
		} else {
			switch mtlsParams.Mtls.GetMode() {
			case v1alpha1.MutualTls_PERMISSIVE:
				mode = ModePermissive
			case v1alpha1.MutualTls_STRICT:
				mode = ModeStrict
			default:
				// Shouldn't happen!
				return mode, fmt.Errorf("unknown MTLS mode when analyzing policy: %s", mtlsParams.Mtls.GetMode().String())
			}
		}
		// Now check for modifiers that might downgrade strict to permissive.
		if mode == ModeStrict && mtlsParams.Mtls != nil && mtlsParams.Mtls.AllowTls {
			mode = ModePermissive
		}
		if mode == ModeStrict && p.PeerIsOptional {
			mode = ModePermissive
		}
		return mode, nil
	}

	// No MTLS configuration found
	return ModePlaintext, nil
}
