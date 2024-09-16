//go:build windows
// +build windows

package iptables

import (
	"encoding/json"
	"net/netip"
	"strconv"

	"github.com/Microsoft/hcsshim/hcn"
	istiolog "istio.io/istio/pkg/log"
)

type WFPConfigurator struct {
}

func (w *WFPConfigurator) CreateInpodRules(logger *istiolog.Scope, endpointID string, hostSNATIPv4, hostSNATIPv6 *netip.Addr) error {
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
		OutboundProxyPort: strconv.Itoa(ZtunnelInboundPlaintextPort),
		UserSID:           "S-1-5-18", // user local sid
		FilterTuple: hcn.FiveTuple{
			RemotePorts: strconv.Itoa(ZtunnelInboundPort),
			Protocols:   "6",
			Priority:    1,
		},
		InboundExceptions: hcn.ProxyExceptions{
			IpAddressExceptions: []string{(*hostSNATIPv4).String(), Localhost},
		},
		OutboundExceptions: hcn.ProxyExceptions{
			IpAddressExceptions: []string{(*hostSNATIPv4).String(), Localhost},
		},
	}

	data, _ := json.Marshal(&policySetting)

	endpointPolicy := hcn.EndpointPolicy{
		Type:     hcn.L4WFPPROXY,
		Settings: data,
	}

	request := hcn.PolicyEndpointRequest{
		Policies: []hcn.EndpointPolicy{endpointPolicy},
	}

	return endpoint.ApplyPolicy(hcn.RequestTypeAdd, request)
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

func (w *WFPConfigurator) DeleteInpodRules(endpointID string) error {
	// @TODO implement:
	/*
		Discover the correct NS for the pod
		drop all policies for the pod ?
	*/
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

	return hcn.ModifyEndpointSettings(endpointID, modifyReq)
}
