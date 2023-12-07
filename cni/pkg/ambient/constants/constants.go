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

package constants

const (
	// In the below, we add the fwmask to ensure only that mark can match
	OutboundMask       = "0x100"
	OutboundMark       = OutboundMask + "/" + OutboundMask
	SkipMask           = "0x200"
	SkipMark           = SkipMask + "/" + SkipMask
	ConnSkipMask       = "0x220"
	ConnSkipMark       = ConnSkipMask + "/" + ConnSkipMask
	ProxyMask          = "0x210"
	ProxyMark          = ProxyMask + "/" + ProxyMask
	ProxyRetMask       = "0x040"
	ProxyRetMark       = ProxyRetMask + "/" + ProxyRetMask
	TProxyMark         = 0x400
	TProxyMask         = 0xfff
	TProxyMarkPriority = 20000
	OrgSrcRetMark      = 0x4d3
	OrgSrcRetMask      = 0xfff
	OrgSrcPriority     = 20003
	EBPFInboundMark    = "5678"
	EBPFOutboundMark   = "8765"

	InboundTun  = "istioin"
	OutboundTun = "istioout"

	InboundTunVNI  = uint32(1000)
	OutboundTunVNI = uint32(1001)

	InboundTunIP         = "192.168.126.1"
	ZTunnelInboundTunIP  = "192.168.126.2"
	OutboundTunIP        = "192.168.127.1"
	ZTunnelOutboundTunIP = "192.168.127.2"
	TunPrefix            = 30

	ChainZTunnelPrerouting  = "ztunnel-PREROUTING"
	ChainZTunnelPostrouting = "ztunnel-POSTROUTING"
	ChainZTunnelInput       = "ztunnel-INPUT"
	ChainZTunnelOutput      = "ztunnel-OUTPUT"
	ChainZTunnelForward     = "ztunnel-FORWARD"

	ChainPrerouting  = "PREROUTING"
	ChainPostrouting = "POSTROUTING"
	ChainInput       = "INPUT"
	ChainOutput      = "OUTPUT"
	ChainForward     = "FORWARD"

	TableMangle = "mangle"
	TableNat    = "nat"
	TableFilter = "filter"

	DNSCapturePort              = 15053
	ZtunnelInboundPort          = 15008
	ZtunnelOutboundPort         = 15001
	ZtunnelInboundPlaintextPort = 15006
)

const (
	RouteTableInbound  = 100
	RouteTableOutbound = 101
	RouteTableProxy    = 102
)

const (
	AmbientConfigFilepath = "/etc/ambient-config/config.json"
	NetNsPath             = "/var/run/netns"
)
