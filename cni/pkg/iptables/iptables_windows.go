// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package iptables

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Microsoft/hcsshim/hcn"
	istiolog "istio.io/istio/pkg/log"
)

type EndpointsFinder interface {
	GetEndpointsForNamespaceGUID(guid string) ([]string, error)
}
type WFPConfigurator struct {
	EndpointsFinder EndpointsFinder
	Cfg             *IptablesConfig
}

func (w *WFPConfigurator) CreateInpodRules(logger *istiolog.Scope, podOverrides PodLevelOverrides, netNsGuid string) error {
	endpointIDs, err := w.EndpointsFinder.GetEndpointsForNamespaceGUID(netNsGuid)
	if err != nil {
		return fmt.Errorf("failed to get endpoints for namespace guid %s: %v", netNsGuid, err)
	}
	// @TODO implement:
	/*
		Discover the correct NS for the pod
		Write WFP policy as per: https://github.com/microsoft/hcnproxyctrl/blob/master/proxy/hcnproxyctl.go
		Ensure health probes are NOT redirected into ztunnel port
		 	// proxyExceptions > IPAddressException? from hcn
		Ignore packets intented for ztunnel port directly
			// proxyExceptions > PortException? from hcn
		Ignore packets to localhost? (maybe?)
	*/
	if len(endpointIDs) == 0 {
		return fmt.Errorf("missing endpointIDs, unable to create Inpod routing policies")
	}

	for _, endpointID := range endpointIDs {
		endpoint, err := hcn.GetEndpointByID(endpointID)
		if err != nil {
			return err
		}

		// nothing to do if we already have a policy
		if w.hasPolicyApplied(endpoint) {
			return nil
		}

		policySetting := hcn.L4WfpProxyPolicySetting{
			InboundProxyPort:  strconv.Itoa(ZtunnelInboundPort),
			OutboundProxyPort: strconv.Itoa(ZtunnelOutboundPort),
			UserSID:           "S-1-5-18", // user local sid
			FilterTuple: hcn.FiveTuple{
				RemotePorts: strconv.Itoa(ZtunnelInboundPort),
				Protocols:   "6",
			},
			InboundExceptions: hcn.ProxyExceptions{
				IpAddressExceptions: []string{w.Cfg.HostProbeSNATAddress.String(), Localhost},
			},
			OutboundExceptions: hcn.ProxyExceptions{
				IpAddressExceptions: []string{w.Cfg.HostProbeSNATAddress.String(), Localhost},
			},
		}

		dataP1, _ := json.Marshal(&policySetting)
		endpointPolicy1 := hcn.EndpointPolicy{
			Type:     hcn.L4WFPPROXY,
			Settings: dataP1,
		}
		// 2nd policy
		policySetting.FilterTuple.RemotePorts = ""
		policySetting.InboundProxyPort = strconv.Itoa(ZtunnelInboundPlaintextPort)
		policySetting.OutboundProxyPort = strconv.Itoa(ZtunnelOutboundPort)
		policySetting.InboundExceptions.PortExceptions = []string{strconv.Itoa(ZtunnelInboundPort)}

		dataP2, _ := json.Marshal(&policySetting)
		endpointPolicy2 := hcn.EndpointPolicy{
			Type:     hcn.L4WFPPROXY,
			Settings: dataP2,
		}

		request := hcn.PolicyEndpointRequest{
			Policies: []hcn.EndpointPolicy{endpointPolicy1, endpointPolicy2},
		}

		err = endpoint.ApplyPolicy(hcn.RequestTypeAdd, request)
		if err != nil {
			return err
		}
	}

	return nil
}

func (w *WFPConfigurator) hasPolicyApplied(endpoint *hcn.HostComputeEndpoint) bool {
	for _, policy := range endpoint.Policies {
		if policy.Type == hcn.L4WFPPROXY {
			return true
		}
	}
	return false
}

func (w *WFPConfigurator) getPoliciesToRemove(endpoint *hcn.HostComputeEndpoint) []hcn.EndpointPolicy {
	deletePol := []hcn.EndpointPolicy{}

	for _, policy := range endpoint.Policies {
		if policy.Type == hcn.L4WFPPROXY {
			deletePol = append(deletePol, policy)
		}
	}

	return deletePol
}

func (w *WFPConfigurator) DeleteInpodRules(_ *istiolog.Scope, netNs string) error {
	// TODO: should we drop all policies for the pod?
	endpointIDs, err := w.EndpointsFinder.GetEndpointsForNamespaceGUID(netNs)
	if err != nil {
		return fmt.Errorf("failed to get endpoints for namespace guid %s: %v", netNs, err)
	}
	for _, endpointID := range endpointIDs {
		endpoint, err := hcn.GetEndpointByID(endpointID)
		if err != nil {
			return err
		}

		delPolicies := w.getPoliciesToRemove(endpoint)
		policyReq := hcn.PolicyEndpointRequest{
			Policies: delPolicies,
		}

		policyJSON, err := json.Marshal(policyReq)
		if err != nil {
			return err
		}

		modifyReq := &hcn.ModifyEndpointSettingRequest{
			ResourceType: hcn.EndpointResourceTypePolicy,
			RequestType:  hcn.RequestTypeRemove,
			Settings:     policyJSON,
		}

		err = hcn.ModifyEndpointSettings(endpointID, modifyReq)
		if err != nil {
			return err
		}
	}

	return nil
}

func (w *WFPConfigurator) ReconcileModeEnabled() bool {
	return w.Cfg.Reconcile
}
