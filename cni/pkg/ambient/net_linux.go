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

package ambient

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	netns "github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/cni/pkg/ambient/constants"
	ebpf "istio.io/istio/cni/pkg/ebpf/server"
	"istio.io/istio/pkg/util/sets"
)

func IsPodInIpset(pod *corev1.Pod) bool {
	ipset, err := Ipset.List()
	if err != nil {
		log.Errorf("Failed to list ipset entries: %v", err)
		return false
	}

	// Since not all kernels support comments in ipset, we should also try and
	// match against the IP
	for _, ip := range ipset {
		if ip.Comment == string(pod.UID) {
			return true
		}
		if ip.IP.String() == pod.Status.PodIP {
			return true
		}
	}

	return false
}

func buildEbpfArgsByIP(ip string, isZtunnel, isRemove bool) (*ebpf.RedirectArgs, error) {
	ipAddr, err := netip.ParseAddr(ip)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ip(%s): %v", ip, err)
	}
	veth, err := getVethWithDestinationOf(ip)
	if err != nil {
		return nil, fmt.Errorf("failed to get device: %v", err)
	}
	peerIndex, err := getPeerIndex(veth)
	if err != nil {
		return nil, fmt.Errorf("failed to get veth peerIndex: %v", err)
	}

	peerNs, err := getNsNameFromNsID(veth.Attrs().NetNsID)
	if err != nil {
		return nil, fmt.Errorf("failed to get ns name: %v", err)
	}

	mac, err := getMacFromNsIdx(peerNs, peerIndex)
	if err != nil {
		return nil, err
	}

	return &ebpf.RedirectArgs{
		IPAddrs:   []netip.Addr{ipAddr},
		MacAddr:   mac,
		Ifindex:   veth.Attrs().Index,
		PeerIndex: peerIndex,
		PeerNs:    peerNs,
		IsZtunnel: isZtunnel,
		Remove:    isRemove,
	}, nil
}

func (s *Server) updateNodeProxyEBPF(pod *corev1.Pod, captureDNS bool) error {
	if s.ebpfServer == nil {
		return fmt.Errorf("uninitialized ebpf server")
	}

	ip := pod.Status.PodIP

	veth, err := getVethWithDestinationOf(ip)
	if err != nil {
		log.Warnf("failed to get device: %v", err)
	}
	peerIndex, err := getPeerIndex(veth)
	if err != nil {
		return fmt.Errorf("failed to get veth peerIndex: %v", err)
	}

	err = disableRPFiltersForLink(veth.Attrs().Name)
	if err != nil {
		log.Warnf("failed to disable procfs rp_filter for device %s: %v", veth.Attrs().Name, err)
	}

	args, err := buildEbpfArgsByIP(ip, true, false)
	if err != nil {
		return err
	}
	args.CaptureDNS = captureDNS
	log.Debugf("update ztunnel ebpf args: %+v", args)

	// Now that we have the ip, the veth, and the ztunnel netns,
	// two things need to happen:
	// 1. We need to interact with the kernel to jump into the ztunnel net namespace
	// and create some local rules within that net namespace
	err = s.CreateEBPFRulesWithinNodeProxyNS(peerIndex, ip, args.PeerNs)
	if err != nil {
		return fmt.Errorf("failed to configure ztunnel pod rules: %v", err)
	}

	// 2. We need to interact with the kernel to attach eBPF progs to ztunnel
	s.ebpfServer.AcceptRequest(args)

	return nil
}

func (s *Server) delZtunnelEbpfOnNode() error {
	if s.ebpfServer == nil {
		return fmt.Errorf("uninitialized ebpf server")
	}

	args := &ebpf.RedirectArgs{
		Ifindex:   0,
		IsZtunnel: true,
		Remove:    true,
	}
	log.Debugf("del ztunnel ebpf args: %+v", args)
	s.ebpfServer.AcceptRequest(args)
	return nil
}

func (s *Server) updatePodEbpfOnNode(pod *corev1.Pod) error {
	if s.ebpfServer == nil {
		return fmt.Errorf("uninitialized ebpf server")
	}

	ip := pod.Status.PodIP
	if ip == "" {
		log.Debugf("skip adding pod %s/%s, IP not yet allocated", pod.Name, pod.Namespace)
		return nil
	}

	args, err := buildEbpfArgsByIP(ip, false, false)
	if err != nil {
		return err
	}

	log.Debugf("update POD ebpf args: %+v", args)
	s.ebpfServer.AcceptRequest(args)
	return nil
}

func (s *Server) delPodEbpfOnNode(ip string, force bool) error {
	if s.ebpfServer == nil {
		return fmt.Errorf("uninitialized ebpf server")
	}

	if ip == "" {
		log.Debugf("nothing could be performed to delete ebpf for empty ip")
		return nil
	}
	ipAddr, err := netip.ParseAddr(ip)
	if err != nil {
		return fmt.Errorf("failed to parse ip(%s): %v", ip, err)
	}

	ifIndex := 0

	if veth, err := getVethWithDestinationOf(ip); err != nil {
		log.Debugf("failed to get device: %v", err)
	} else {
		ifIndex = veth.Attrs().Index
	}

	if force {
		return s.ebpfServer.RemovePod([]netip.Addr{ipAddr}, uint32(ifIndex))
	}
	args := &ebpf.RedirectArgs{
		IPAddrs:   []netip.Addr{ipAddr},
		Ifindex:   ifIndex,
		IsZtunnel: false,
		Remove:    true,
	}
	log.Debugf("del POD ebpf args: %+v", args)
	s.ebpfServer.AcceptRequest(args)
	return nil
}

func getLinkWithDestinationOf(ip string) (netlink.Link, error) {
	routes, err := netlink.RouteListFiltered(
		netlink.FAMILY_V4,
		&netlink.Route{Dst: &net.IPNet{IP: net.ParseIP(ip), Mask: net.CIDRMask(32, 32)}},
		netlink.RT_FILTER_DST)
	if err != nil {
		return nil, err
	}

	if len(routes) == 0 {
		return nil, fmt.Errorf("no routes found for %s", ip)
	}

	linkIndex := routes[0].LinkIndex
	return netlink.LinkByIndex(linkIndex)
}

func getVethWithDestinationOf(ip string) (*netlink.Veth, error) {
	link, err := getLinkWithDestinationOf(ip)
	if err != nil {
		return nil, err
	}
	veth, ok := link.(*netlink.Veth)
	if !ok {
		return nil, errors.New("not veth implemented CNI")
	}
	return veth, nil
}

func getDeviceWithDestinationOf(ip string) (string, error) {
	link, err := getLinkWithDestinationOf(ip)
	if err != nil {
		return "", err
	}
	return link.Attrs().Name, nil
}

func GetIndexAndPeerMac(podIfName, ns string) (int, net.HardwareAddr, error) {
	var hostIfIndex int
	var hwAddr net.HardwareAddr

	ns = filepath.Base(ns)
	err := netns.WithNetNSPath(fmt.Sprintf("/var/run/netns/%s", ns), func(netns.NetNS) error {
		link, err := netlink.LinkByName(podIfName)
		if err != nil {
			return err
		}

		veth, ok := link.(*netlink.Veth)
		if !ok {
			return fmt.Errorf("not veth implemented CNI")
		}

		ifIndex, err := netlink.VethPeerIndex(veth)
		if err != nil {
			return err
		}

		hwAddr = veth.Attrs().HardwareAddr
		hostIfIndex = ifIndex
		return nil
	})
	if err != nil {
		return 0, nil, fmt.Errorf("failed to get info for if(%s) in ns(%s): %v", podIfName, ns, err)
	}

	return hostIfIndex, hwAddr, nil
}

func getMacFromNsIdx(ns string, ifIndex int) (net.HardwareAddr, error) {
	var hwAddr net.HardwareAddr
	err := netns.WithNetNSPath(fmt.Sprintf("/var/run/netns/%s", ns), func(netns.NetNS) error {
		link, err := netlink.LinkByIndex(ifIndex)
		if err != nil {
			return fmt.Errorf("failed to get link(%d) in ns(%s): %v", ifIndex, ns, err)
		}
		hwAddr = link.Attrs().HardwareAddr
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hwAddr, nil
}

func getNsNameFromNsID(nsid int) (string, error) {
	foundNs := errors.New("nsid found, stop iterating")
	nsName := ""
	err := filepath.WalkDir("/var/run/netns", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fd, err := unix.Open(p, unix.O_RDONLY, 0)
		if err != nil {
			log.Warnf("failed to open: %v", err)
			return nil
		}
		defer unix.Close(fd)

		id, err := netlink.GetNetNsIdByFd(fd)
		if err != nil {
			log.Warnf("failed to open: %v", err)
			return nil
		}
		if id == nsid {
			nsName = path.Base(p)
			return foundNs
		}
		return nil
	})
	if err == foundNs {
		return nsName, nil
	}
	return "", fmt.Errorf("failed to get namespace for %d", nsid)
}

func getPeerIndex(veth *netlink.Veth) (int, error) {
	return netlink.VethPeerIndex(veth)
}

// CreateRulesOnNode initializes the routing, firewall and ipset rules on the node.
func (s *Server) CreateRulesOnNode(ztunnelVeth, ztunnelIP string, captureDNS bool) error {
	var err error

	log.Debugf("CreateRulesOnNode: ztunnelVeth=%s, ztunnelIP=%s", ztunnelVeth, ztunnelIP)

	// Check if chain exists, if it exists flush.. otherwise initialize
	err = execute(s.IptablesCmd(), "-t", "mangle", "-C", "OUTPUT", "-j", constants.ChainZTunnelOutput)
	if err == nil {
		log.Debugf("Chain %s already exists, flushing", constants.ChainOutput)
		s.flushLists()
	} else {
		log.Debugf("Initializing lists")
		err = s.initializeLists()
		if err != nil {
			return err
		}
	}

	// Create ipset of pod members.
	log.Debug("Creating ipset")
	err = Ipset.CreateSet()
	if err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("error creating ipset: %v", err)
	}

	appendRules := []*iptablesRule{
		// Skip things that come from the tunnels, but don't apply the conn skip mark
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelPrerouting,
			"-i", constants.InboundTun,
			"-j", "MARK",
			"--set-mark", constants.SkipMark,
		),
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelPrerouting,
			"-i", constants.InboundTun,
			"-j", "RETURN",
		),
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelPrerouting,
			"-i", constants.OutboundTun,
			"-j", "MARK",
			"--set-mark", constants.SkipMark,
		),
		newIptableRule(constants.TableMangle,
			constants.ChainZTunnelPrerouting,
			"-i", constants.OutboundTun,
			"-j", "RETURN",
		),

		// Make sure that whatever is skipped is also skipped for returning packets.
		// If we have a skip mark, save it to conn mark.
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelForward,
			"-m", "mark",
			"--mark", constants.ConnSkipMark,
			"-j", "CONNMARK",
			"--save-mark",
			"--nfmask", constants.ConnSkipMask,
			"--ctmask", constants.ConnSkipMask,
		),
		// Input chain might be needed for things in host namespace that are skipped.
		// Place the mark here after routing was done, not sure if conn-tracking will figure
		// it out if I do it before, as NAT might change the connection tuple.
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelInput,
			"-m", "mark",
			"--mark", constants.ConnSkipMark,
			"-j", "CONNMARK",
			"--save-mark",
			"--nfmask", constants.ConnSkipMask,
			"--ctmask", constants.ConnSkipMask,
		),

		// For things with the proxy mark, we need different routing just on returning packets
		// so we give a different mark to them.
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelForward,
			"-m", "mark",
			"--mark", constants.ProxyMark,
			"-j", "CONNMARK",
			"--save-mark",
			"--nfmask", constants.ProxyMask,
			"--ctmask", constants.ProxyMask,
		),
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelInput,
			"-m", "mark",
			"--mark", constants.ProxyMark,
			"-j", "CONNMARK",
			"--save-mark",
			"--nfmask", constants.ProxyMask,
			"--ctmask", constants.ProxyMask,
		),
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelOutput,
			"--source", HostIP,
			"-j", "MARK",
			"--set-mark", constants.ConnSkipMask,
		),

		// If we have an outbound mark, we don't need kube-proxy to do anything,
		// so accept it before kube-proxy translates service vips to pod ips
		newIptableRule(
			constants.TableNat,
			constants.ChainZTunnelPrerouting,
			"-m", "mark",
			"--mark", constants.OutboundMark,
			"-j", "ACCEPT",
		),
		newIptableRule(
			constants.TableNat,
			constants.ChainZTunnelPostrouting,
			"-m", "mark",
			"--mark", constants.OutboundMark,
			"-j", "ACCEPT",
		),

		// If we have an outbound mark, we don't need kube-proxy to do anything,
		// so accept it before the KUBE-SERVICES chain in the Filter table rejects
		// destinations that do not have a k8s endpoint resource. This is necessary
		// for traffic destined to workloads outside k8s whose endpoints are known
		// to ZTunnel but for whom k8s endpoints resources do not exist.
		newIptableRule(
			constants.TableFilter,
			constants.ChainZTunnelForward,
			"-m", "mark",
			"--mark", constants.OutboundMark,
			"-j", "ACCEPT",
		),
	}

	if captureDNS {
		appendRules = append(appendRules,
			newIptableRule(
				constants.TableNat,
				constants.ChainZTunnelPrerouting,
				"-p", "udp",
				"-m", "set",
				"--match-set", Ipset.Name, "src",
				"--dport", "53",
				"-j", "DNAT",
				"--to", fmt.Sprintf("%s:%d", ztunnelIP, constants.DNSCapturePort),
			),
		)
	}

	appendRules2 := []*iptablesRule{
		// Don't set anything on the tunnel (geneve port is 6081), as the tunnel copies
		// the mark to the un-tunneled packet.
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelPrerouting,
			"-p", "udp",
			"-m", "udp",
			"--dport", "6081",
			"-j", "RETURN",
		),

		// If we have the conn mark, restore it to mark, to make sure that the other side of the connection
		// is skipped as well.
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelPrerouting,
			"-m", "connmark",
			"--mark", constants.ConnSkipMark,
			"-j", "MARK",
			"--set-mark", constants.SkipMark,
		),
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelPrerouting,
			"-m", "mark",
			"--mark", constants.SkipMark,
			"-j", "RETURN",
		),

		// If we have the proxy mark in, set the return mark to make sure that original src packets go to ztunnel
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelPrerouting,
			"!", "-i", ztunnelVeth,
			"-m", "connmark",
			"--mark", constants.ProxyMark,
			"-j", "MARK",
			"--set-mark", constants.ProxyRetMark,
		),
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelPrerouting,
			"-m", "mark",
			"--mark", constants.ProxyRetMark,
			"-j", "RETURN",
		),

		// Send fake source outbound connections to the outbound route table (for original src)
		// if it's original src, the source ip of packets coming from the proxy might be that of a pod, so
		// make sure we don't tproxy it
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelPrerouting,
			"-i", ztunnelVeth,
			"!", "--source", ztunnelIP,
			"-j", "MARK",
			"--set-mark", constants.ProxyMark,
		),
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelPrerouting,
			"-m", "mark",
			"--mark", constants.SkipMark,
			"-j", "RETURN",
		),

		// Make sure anything that leaves ztunnel is routed normally (xds, connections to other ztunnels,
		// connections to upstream pods...)
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelPrerouting,
			"-i", ztunnelVeth,
			"-j", "MARK",
			"--set-mark", constants.ConnSkipMark,
		),

		// skip udp so DNS works. We can make this more granular.
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelPrerouting,
			"-p", "udp",
			"-j", "MARK",
			"--set-mark", constants.ConnSkipMark,
		),

		// Skip things from host ip - these are usually kubectl probes
		// skip anything with skip mark. This can be used to add features like port exclusions
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelPrerouting,
			"-m", "mark",
			"--mark", constants.SkipMark,
			"-j", "RETURN",
		),

		// Mark outbound connections to route them to the proxy using ip rules/route tables
		// Per Yuval, interface_prefix can be left off this rule... but we should check this (hard to automate
		// detection).
		newIptableRule(
			constants.TableMangle,
			constants.ChainZTunnelPrerouting,
			"-p", "tcp",
			"-m", "set",
			"--match-set", Ipset.Name, "src",
			"-j", "MARK",
			"--set-mark", constants.OutboundMark,
		),
	}

	err = s.iptablesAppend(appendRules)
	if err != nil {
		log.Errorf("failed to append iptables rule: %v", err)
	}

	err = s.iptablesAppend(appendRules2)
	if err != nil {
		log.Errorf("failed to append iptables rule: %v", err)
	}

	// Need to do some work in procfs
	// @TODO: This likely needs to be cleaned up, there are a lot of martians in AWS
	// that seem to necessitate this work.
	procs := map[string]int{
		"/proc/sys/net/ipv4/conf/default/rp_filter":                0,
		"/proc/sys/net/ipv4/conf/all/rp_filter":                    0,
		"/proc/sys/net/ipv4/conf/" + ztunnelVeth + "/rp_filter":    0,
		"/proc/sys/net/ipv4/conf/" + ztunnelVeth + "/accept_local": 1,
	}
	for proc, val := range procs {
		err = SetProc(proc, fmt.Sprint(val))
		if err != nil {
			log.Errorf("failed to write to proc file %s: %v", proc, err)
		}
	}

	// Create tunnels
	inbnd := &netlink.Geneve{
		LinkAttrs: netlink.LinkAttrs{
			Name: constants.InboundTun,
		},
		ID:     1000,
		Remote: net.ParseIP(ztunnelIP),
	}
	log.Debugf("Building inbound tunnel: %+v", inbnd)
	err = netlink.LinkAdd(inbnd)
	if err != nil {
		log.Errorf("failed to add inbound tunnel: %v", err)
	}
	err = netlink.AddrAdd(inbnd, &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   net.ParseIP(constants.InboundTunIP),
			Mask: net.CIDRMask(constants.TunPrefix, 32),
		},
	})
	if err != nil {
		log.Errorf("failed to add inbound tunnel address: %v", err)
	}

	outbnd := &netlink.Geneve{
		LinkAttrs: netlink.LinkAttrs{
			Name: constants.OutboundTun,
		},
		ID:     1001,
		Remote: net.ParseIP(ztunnelIP),
	}
	log.Debugf("Building outbound tunnel: %+v", outbnd)
	err = netlink.LinkAdd(outbnd)
	if err != nil {
		log.Errorf("failed to add outbound tunnel: %v", err)
	}
	err = netlink.AddrAdd(outbnd, &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   net.ParseIP(constants.OutboundTunIP),
			Mask: net.CIDRMask(constants.TunPrefix, 32),
		},
	})
	if err != nil {
		log.Errorf("failed to add outbound tunnel address: %v", err)
	}

	err = netlink.LinkSetUp(inbnd)
	if err != nil {
		log.Errorf("failed to set inbound tunnel up: %v", err)
	}
	err = netlink.LinkSetUp(outbnd)
	if err != nil {
		log.Errorf("failed to set outbound tunnel up: %v", err)
	}

	procs = map[string]int{
		"/proc/sys/net/ipv4/conf/" + constants.InboundTun + "/rp_filter":     0,
		"/proc/sys/net/ipv4/conf/" + constants.InboundTun + "/accept_local":  1,
		"/proc/sys/net/ipv4/conf/" + constants.OutboundTun + "/rp_filter":    0,
		"/proc/sys/net/ipv4/conf/" + constants.OutboundTun + "/accept_local": 1,
	}
	for proc, val := range procs {
		err = SetProc(proc, fmt.Sprint(val))
		if err != nil {
			log.Errorf("failed to write to proc file %s: %v", proc, err)
		}
	}

	dirEntries, err := os.ReadDir("/proc/sys/net/ipv4/conf")
	if err != nil {
		log.Errorf("failed to read /proc/sys/net/ipv4/conf: %v", err)
	}
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() {
			if _, err := os.Stat("/proc/sys/net/ipv4/conf/" + dirEntry.Name() + "/rp_filter"); err != nil {
				err := SetProc("/proc/sys/net/ipv4/conf/"+dirEntry.Name()+"/rp_filter", "0")
				if err != nil {
					log.Errorf("failed to set /proc/sys/net/ipv4/conf/%s/rp_filter: %v", dirEntry.Name(), err)
				}
			}
		}
	}

	routes := []*ExecList{
		newExec("ip",
			[]string{
				"route", "add", "table", fmt.Sprint(constants.RouteTableOutbound), ztunnelIP,
				"dev", ztunnelVeth, "scope", "link",
			},
		),
		newExec("ip",
			[]string{
				"route", "add", "table", fmt.Sprint(constants.RouteTableOutbound), "0.0.0.0/0",
				"via", constants.ZTunnelOutboundTunIP, "dev", constants.OutboundTun,
			},
		),
		newExec("ip",
			[]string{
				"route", "add", "table", fmt.Sprint(constants.RouteTableProxy), ztunnelIP,
				"dev", ztunnelVeth, "scope", "link",
			},
		),
		newExec("ip",
			[]string{
				"route", "add", "table", fmt.Sprint(constants.RouteTableProxy), "0.0.0.0/0",
				"via", ztunnelIP, "dev", ztunnelVeth, "onlink",
			},
		),
		newExec("ip",
			[]string{
				"route", "add", "table", fmt.Sprint(constants.RouteTableInbound), ztunnelIP,
				"dev", ztunnelVeth, "scope", "link",
			},
		),
		// Everything with the skip mark goes directly to the main table
		newExec("ip",
			[]string{
				"rule", "add", "priority", "100",
				"fwmark", fmt.Sprint(constants.SkipMark),
				"goto", "32766",
			},
		),
		// Everything with the outbound mark goes to the tunnel out device
		// using the outbound route table
		newExec("ip",
			[]string{
				"rule", "add", "priority", "101",
				"fwmark", fmt.Sprint(constants.OutboundMark),
				"lookup", fmt.Sprint(constants.RouteTableOutbound),
			},
		),
		// Things with the proxy return mark go directly to the proxy veth using the proxy
		// route table (useful for original src)
		newExec("ip",
			[]string{
				"rule", "add", "priority", "102",
				"fwmark", fmt.Sprint(constants.ProxyRetMark),
				"lookup", fmt.Sprint(constants.RouteTableProxy),
			},
		),
		// Send all traffic to the inbound table. This table has routes only to pods in the mesh.
		// It does not have a catch-all route, so if a route is missing, the search will continue
		// allowing us to override routing just for member pods.
		newExec("ip",
			[]string{
				"rule", "add", "priority", "103",
				"table", fmt.Sprint(constants.RouteTableInbound),
			},
		),
	}

	for _, route := range routes {
		err = execute(route.Cmd, route.Args...)
		if err != nil {
			log.Errorf(fmt.Errorf("failed to add route (%+v): %v", route, err))
		}
	}

	return nil
}

// CreateEBPFRulesInNodeProxyNS initializes the routes and iptable rules that need to exist WITHIN
// the node proxy (ztunnel) netns - this is distinct from the routes and rules that need to exist OUTSIDE
// of the node proxy netns, on the node, which are handled elsewhere.
//
// There is no cleanup required for things we do within the netns, as when the netns is destroyed on pod delete,
// everything within the netns goes away.
func (s *Server) CreateEBPFRulesWithinNodeProxyNS(proxyNsVethIdx int, ztunnelIP, ztunnelNetNS string) error {
	ns := filepath.Base(ztunnelNetNS)
	log.Debugf("CreateEBPFRulesWithinNodeProxyNS: proxyNsVethIdx=%d, ztunnelIP=%s, from within netns=%s", proxyNsVethIdx, ztunnelIP, ztunnelNetNS)
	err := netns.WithNetNSPath(fmt.Sprintf("/var/run/netns/%s", ns), func(netns.NetNS) error {
		// Make sure we flush table 100 before continuing - it should be empty in a new namespace
		// but better to ensure that.
		if err := routeFlushTable(constants.RouteTableInbound); err != nil {
			log.Error(err)
		}

		// Flush rules before initializing within 'addTProxyMarks'
		deleteIPRules([]string{strconv.Itoa(constants.TProxyMarkPriority), strconv.Itoa(constants.OrgSrcPriority)}, false)

		// Set up tproxy marks
		err := addTProxyMarkRule()
		if err != nil {
			return fmt.Errorf("failed to add TPROXY mark rules: %v", err)
		}

		loopbackLink, err := netlink.LinkByName("lo")
		if err != nil {
			return fmt.Errorf("failed to find 'lo' link: %v", err)
		}
		// In routing table ${INBOUND_TPROXY_ROUTE_TABLE}, create a single default rule to route all traffic to
		// the loopback interface.
		// Equiv: "ip route add local 0.0.0.0/0 dev lo table 100"
		// TODO IPv6, append "0::0/0"
		cidrs := []string{"0.0.0.0/0"}
		for _, fullCIDR := range cidrs {
			_, dst, err := net.ParseCIDR(fullCIDR)
			if err != nil {
				return fmt.Errorf("parse CIDR: %v", err)
			}

			if err := netlink.RouteAdd(&netlink.Route{
				Dst:       dst,
				Scope:     netlink.SCOPE_HOST,
				Type:      unix.RTN_LOCAL,
				Table:     constants.RouteTableInbound,
				LinkIndex: loopbackLink.Attrs().Index,
			}); err != nil {
				// TODO clear this route every time
				// Would not expect this if we have properly cleared routes
				return fmt.Errorf("failed to add route: %v", err)
			}
		}

		vethLink, err := netlink.LinkByIndex(proxyNsVethIdx)
		if err != nil {
			return fmt.Errorf("failed to find veth with index '%d' within namespace %s: %v", proxyNsVethIdx, ztunnelNetNS, err)
		}

		err = disableRPFiltersForLink(vethLink.Attrs().Name)
		if err != nil {
			log.Warnf("failed to disable procfs rp_filter for device %s: %v", vethLink.Attrs().Name, err)
		}

		if ebpf.EBPFTProxySupport() {
			return nil
		}
		log.Infof("Current kernel doesn't support tproxy in eBPF, fall back to iptables tproxy rules")
		return s.createTProxyRulesForLegacyEBPF(ztunnelIP, vethLink.Attrs().Name)
	})
	if err != nil {
		return fmt.Errorf("failed to configure ztunnel ebpf from within ns(%s): %v", ns, err)
	}

	return nil
}

func (s *Server) createTProxyRulesForLegacyEBPF(ztunnelIP, ifName string) error {
	err := addOrgSrcMarkRule()
	if err != nil {
		return fmt.Errorf("failed to add OrgSrc mark rules: %v", err)
	}

	// Flush prerouting table - this should be a new pod netns and it should be clean, but just to be safe..
	err = execute(s.IptablesCmd(), "-t", "mangle", "-F", "PREROUTING")
	if err != nil {
		return fmt.Errorf("failed to configure iptables rule: %v", err)
	}

	// Set up append rules
	appendRules := []*iptablesRule{
		// Set eBPF mark on inbound packets
		newIptableRule(
			constants.TableMangle,
			"PREROUTING",
			"-p", "tcp",
			"-m", "mark",
			"--mark", constants.EBPFInboundMark,
			"-m", "tcp",
			"--dport", fmt.Sprintf("%d", constants.ZtunnelInboundPort),
			"-j", "TPROXY",
			"--tproxy-mark", fmt.Sprintf("0x%x", constants.TProxyMark)+"/"+fmt.Sprintf("0x%x", constants.TProxyMask),
			"--on-port", fmt.Sprintf("%d", constants.ZtunnelInboundPort),
			"--on-ip", "127.0.0.1",
		),
		// Same mark, but on plaintext port
		newIptableRule(
			constants.TableMangle,
			"PREROUTING",
			"-p", "tcp",
			"-m", "mark",
			"--mark", constants.EBPFInboundMark,
			"-j", "TPROXY",
			"--tproxy-mark", fmt.Sprintf("0x%x", constants.TProxyMark)+"/"+fmt.Sprintf("0x%x", constants.TProxyMask),
			"--on-port", fmt.Sprintf("%d", constants.ZtunnelInboundPlaintextPort),
			"--on-ip", "127.0.0.1",
		),
		// Set outbound eBPF mark
		newIptableRule(
			constants.TableMangle,
			"PREROUTING",
			"-p", "tcp",
			"-m", "mark",
			"--mark", constants.EBPFOutboundMark,
			"-j", "TPROXY",
			"--tproxy-mark", fmt.Sprintf("0x%x", constants.TProxyMark)+"/"+fmt.Sprintf("0x%x", constants.TProxyMask),
			"--on-port", fmt.Sprintf("%d", constants.ZtunnelOutboundPort),
			"--on-ip", "127.0.0.1",
		),
		// For anything NOT going to the ztunnel IP, add the OrgSrcRet mark
		newIptableRule(
			constants.TableMangle,
			"PREROUTING",
			"-p", "tcp",
			"-i", ifName,
			"!",
			"--dst", ztunnelIP,
			"-j", "MARK",
			"--set-mark", fmt.Sprintf("0x%x", constants.OrgSrcRetMark)+"/"+fmt.Sprintf("0x%x", constants.OrgSrcRetMask),
		),
	}
	if err := s.iptablesAppend(appendRules); err != nil {
		return fmt.Errorf("failed to append iptables rule: %v", err)
	}

	return nil
}

// CreateRulesWithinNodeProxyNS initializes the routes and iptable rules that need to exist WITHIN
// the node proxy (ztunnel) netns - this is distinct from the routes and rules that need to exist OUTSIDE
// of the node proxy netns, on the node, which are handled elsewhere.
//
// There is no cleanup required for things we do within the netns, as when the netns is destroyed on pod delete,
// everything within the netns goes away.
func (s *Server) CreateRulesWithinNodeProxyNS(proxyNsVethIdx int, ztunnelIP, ztunnelNetNS, hostIP string) error {
	ns := filepath.Base(ztunnelNetNS)
	log.Debugf("CreateRulesWithinNodeProxyNS: proxyNsVethIdx=%d, ztunnelIP=%s, hostIP=%s, from within netns=%s", proxyNsVethIdx, ztunnelIP, hostIP, ztunnelNetNS)
	err := netns.WithNetNSPath(fmt.Sprintf("/var/run/netns/%s", ns), func(netns.NetNS) error {
		//"p" is just to visually distinguish from the host-side tunnel links in logs
		inboundGeneveLinkName := "p" + constants.InboundTun
		outboundGeneveLinkName := "p" + constants.OutboundTun

		// New pod NS SHOULD be empty - but in case it isn't, flush/clean everything we are
		// about to create, ignoring warnings
		//
		// TODO not strictly necessary? A harmless correctness check, at least.
		flushAllRouteTables()

		deleteIPRules([]string{"20000", "20001", "20002", "20003"}, false)

		deleteTunnelLinks(inboundGeneveLinkName, outboundGeneveLinkName, false)

		// Create INBOUND Geneve tunnel (from host)
		inbndTunLink := &netlink.Geneve{
			LinkAttrs: netlink.LinkAttrs{
				Name: inboundGeneveLinkName,
			},
			ID:     1000,
			Remote: net.ParseIP(hostIP),
		}
		log.Debugf("Building inbound tunnel: %+v", inbndTunLink)
		err := netlink.LinkAdd(inbndTunLink)
		if err != nil {
			log.Errorf("failed to add inbound tunnel: %v", err)
		}
		err = netlink.AddrAdd(inbndTunLink, &netlink.Addr{
			IPNet: &net.IPNet{
				IP:   net.ParseIP(constants.ZTunnelInboundTunIP),
				Mask: net.CIDRMask(constants.TunPrefix, 32),
			},
		})
		if err != nil {
			log.Errorf("failed to add inbound tunnel address: %v", err)
		}

		// Create OUTBOUND Geneve tunnel (to host)
		outbndTunLink := &netlink.Geneve{
			LinkAttrs: netlink.LinkAttrs{
				Name: outboundGeneveLinkName,
			},
			ID:     1001,
			Remote: net.ParseIP(hostIP),
		}
		log.Debugf("Building outbound tunnel: %+v", outbndTunLink)
		err = netlink.LinkAdd(outbndTunLink)
		if err != nil {
			log.Errorf("failed to add outbound tunnel: %v", err)
		}
		err = netlink.AddrAdd(outbndTunLink, &netlink.Addr{
			IPNet: &net.IPNet{
				IP:   net.ParseIP(constants.ZTunnelOutboundTunIP),
				Mask: net.CIDRMask(constants.TunPrefix, 32),
			},
		})
		if err != nil {
			log.Errorf("failed to add outbound tunnel address: %v", err)
		}

		log.Debugf("Bringing up inbound tunnel: %+v", inbndTunLink)
		// Bring the tunnels up
		err = netlink.LinkSetUp(inbndTunLink)
		if err != nil {
			log.Errorf("failed to set inbound tunnel up: %v", err)
		}
		log.Debugf("Bringing up outbound tunnel: %+v", outbndTunLink)
		err = netlink.LinkSetUp(outbndTunLink)
		if err != nil {
			log.Errorf("failed to set outbound tunnel up: %v", err)
		}

		// Turn OFF  reverse packet filtering for the tunnels
		// This is required for iptables impl, but not for eBPF impl
		log.Debugf("Disabling '/rp_filter' for inbound and outbound tunnels")
		procs := map[string]int{
			"/proc/sys/net/ipv4/conf/" + outbndTunLink.Name + "/rp_filter": 0,
			"/proc/sys/net/ipv4/conf/" + inbndTunLink.Name + "/rp_filter":  0,
		}
		for proc, val := range procs {
			err = SetProc(proc, fmt.Sprint(val))
			if err != nil {
				log.Errorf("failed to write to proc file %s: %v", proc, err)
			}
		}

		// Set up tproxy marks
		err = addTProxyMarkRule()
		if err != nil {
			return fmt.Errorf("failed to add TPROXY mark rules: %v", err)
		}
		err = addOrgSrcMarkRule()
		if err != nil {
			return fmt.Errorf("failed to add OrgSrc mark rules: %v", err)
		}

		loopbackLink, err := netlink.LinkByName("lo")
		if err != nil {
			return fmt.Errorf("failed to find 'lo' link: %v", err)
		}

		// Set up netlink routes for localhost
		// TODO IPv6, append "0::0/0"
		cidrs := []string{"0.0.0.0/0"}
		for _, fullCIDR := range cidrs {
			_, localhostDst, err := net.ParseCIDR(fullCIDR)
			if err != nil {
				return fmt.Errorf("parse CIDR: %v", err)
			}

			netlinkRoutes := []*netlink.Route{
				// In routing table ${INBOUND_TPROXY_ROUTE_TABLE}, create a single default rule to route all traffic to
				// the loopback interface.
				// Equiv: "ip route add local 0.0.0.0/0 dev lo table 100"
				{
					Dst:       localhostDst,
					Scope:     netlink.SCOPE_HOST,
					Type:      unix.RTN_LOCAL,
					Table:     constants.RouteTableInbound,
					LinkIndex: loopbackLink.Attrs().Index,
				},
				// Send to localhost, if it came via OutboundTunIP
				// Equiv: "ip route add table 101 0.0.0.0/0 via $OUTBOUND_TUN_IP dev p$OUTBOUND_TUN"
				{
					Dst:       localhostDst,
					Gw:        net.ParseIP(constants.OutboundTunIP),
					Type:      unix.RTN_UNICAST,
					Table:     constants.RouteTableOutbound,
					LinkIndex: outbndTunLink.Attrs().Index,
				},
				// Send to localhost, if it came via InboundTunIP
				// Equiv: "ip route add table 102 0.0.0.0/0 via $INBOUND_TUN_IP dev p$INBOUND_TUN"
				{
					Dst:       localhostDst,
					Gw:        net.ParseIP(constants.InboundTunIP),
					Type:      unix.RTN_UNICAST,
					Table:     constants.RouteTableProxy,
					LinkIndex: inbndTunLink.Attrs().Index,
				},
			}

			for _, route := range netlinkRoutes {
				log.Debugf("Adding netlink route : %+v", route)
				if err := netlink.RouteAdd(route); err != nil {
					// TODO clear this route every time
					// Would not expect this if we have properly cleared routes
					log.Errorf("Failed to add netlink route : %+v", route)
					return fmt.Errorf("failed to add route: %v", err)
				}
			}
		}

		log.Debugf("Finding link and parsing host IP")
		_, parsedHostIPNet, err := net.ParseCIDR(hostIP + "/32")
		if err != nil {
			return fmt.Errorf("could not parse host IP %s: %v", hostIP, err)
		}

		vethLink, err := netlink.LinkByIndex(proxyNsVethIdx)
		if err != nil {
			return fmt.Errorf("failed to find veth with index '%d' within namespace %s: %v", proxyNsVethIdx, ztunnelNetNS, err)
		}
		netlinkHostRoutes := []*netlink.Route{
			// Send to localhost, if it came via InboundTunIP
			// Equiv: "ip route add table 101 $HOST_IP dev eth0 scope link"
			{
				Dst:       parsedHostIPNet,
				Scope:     netlink.SCOPE_LINK,
				Type:      unix.RTN_UNICAST,
				Table:     constants.RouteTableOutbound,
				LinkIndex: vethLink.Attrs().Index,
			},
			// Send to localhost, if it came via InboundTunIP
			// Equiv: "ip route add table 102 $HOST_IP dev eth0 scope link"
			{
				Dst:       parsedHostIPNet,
				Scope:     netlink.SCOPE_LINK,
				Type:      unix.RTN_UNICAST,
				Table:     constants.RouteTableProxy,
				LinkIndex: vethLink.Attrs().Index,
			},
		}

		for _, route := range netlinkHostRoutes {
			log.Debugf("Adding netlink HOST_IP routes : %+v", route)
			if err := netlink.RouteAdd(route); err != nil {
				// TODO clear this route every time
				// Would not expect this if we have properly cleared routes
				return fmt.Errorf("failed to add host route: %v", err)
			}
		}

		log.Debugf("Preparing to apply iptables rules")
		// Flush prerouting and output table - this should be a new pod netns and it should be clean, but just to be safe..
		err = execute(s.IptablesCmd(), "-t", "mangle", "-F", "PREROUTING")
		if err != nil {
			return fmt.Errorf("failed to configure iptables rule: %v", err)
		}
		err = execute(s.IptablesCmd(), "-t", "nat", "-F", "OUTPUT")
		if err != nil {
			return fmt.Errorf("failed to configure iptables rule: %v", err)
		}

		// Set up append rules
		appendRules := []*iptablesRule{
			// Set tproxy mark on anything going to inbound port via the tunnel link and set it to ztunnel
			newIptableRule(
				constants.TableMangle,
				"PREROUTING",
				"-p", "tcp",
				"-i", inbndTunLink.Name,
				"-m", "tcp",
				"--dport", fmt.Sprintf("%d", constants.ZtunnelInboundPort),
				"-j", "TPROXY",
				"--tproxy-mark", fmt.Sprintf("0x%x", constants.TProxyMark)+"/"+fmt.Sprintf("0x%x", constants.TProxyMask),
				"--on-port", fmt.Sprintf("%d", constants.ZtunnelInboundPort),
				"--on-ip", "127.0.0.1",
			),
			// Set tproxy mark on anything coming from outbound tunnel link, and forward it to the ztunnel outbound port
			newIptableRule(
				constants.TableMangle,
				"PREROUTING",
				"-p", "tcp",
				"-i", outbndTunLink.Name,
				"-j", "TPROXY",
				"--tproxy-mark", fmt.Sprintf("0x%x", constants.TProxyMark)+"/"+fmt.Sprintf("0x%x", constants.TProxyMask),
				"--on-port", fmt.Sprintf("%d", constants.ZtunnelOutboundPort),
				"--on-ip", "127.0.0.1",
			),
			// Same mark, but on plaintext port
			newIptableRule(
				constants.TableMangle,
				"PREROUTING",
				"-p", "tcp",
				"-i", inbndTunLink.Name,
				"-j", "TPROXY",
				"--tproxy-mark", fmt.Sprintf("0x%x", constants.TProxyMark)+"/"+fmt.Sprintf("0x%x", constants.TProxyMask),
				"--on-port", fmt.Sprintf("%d", constants.ZtunnelInboundPlaintextPort),
				"--on-ip", "127.0.0.1",
			),
			// For anything NOT going to the ztunnel IP, add the OrgSrcRet mark
			newIptableRule(
				constants.TableMangle,
				"PREROUTING",
				"-p", "tcp",
				"-i", vethLink.Attrs().Name,
				"!",
				"--dst", ztunnelIP,
				"-j", "MARK",
				"--set-mark", fmt.Sprintf("0x%x", constants.OrgSrcRetMark)+"/"+fmt.Sprintf("0x%x", constants.OrgSrcRetMask),
			),
		}

		log.Debugf("Adding iptables rules")
		err = s.iptablesAppend(appendRules)
		if err != nil {
			log.Errorf("failed to append iptables rule: %v", err)
		}

		err = disableRPFiltersForLink(vethLink.Attrs().Name)
		if err != nil {
			log.Warnf("failed to disable procfs rp_filter for device %s: %v", vethLink.Attrs().Name, err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to configure ztunnel via iptables from within ns(%s): %v", ns, err)
	}

	return nil
}

func (s *Server) ztunnelDown() {
	switch s.redirectMode {
	case EbpfMode:
		if err := s.delZtunnelEbpfOnNode(); err != nil {
			log.Error(err)
		}
	case IptablesMode:
		// nothing to do with IptablesMode
	}
}

// Cleans up EVERYTHING on the node.
func (s *Server) cleanupNode() {
	log.Infof("Node-level network rule cleanup started")
	defer log.Infof("Node-level cleanup done")
	log.Infof("If rules do not exist in the first place, warnings will be triggered - these can be safely ignored")
	switch s.redirectMode {
	case EbpfMode:
		if err := s.cleanupPodsEbpfOnNode(); err != nil {
			log.Errorf("%v", err)
		}
	case IptablesMode:
		s.cleanRules()

		flushAllRouteTables()

		deleteIPRules([]string{"100", "101", "102", "103"}, true)

		deleteTunnelLinks(constants.InboundTun, constants.OutboundTun, true)

		err := Ipset.DestroySet()
		if err != nil {
			log.Warnf("unable to delete IPSet: %v", err)
		}
	}
}

func (s *Server) getEnrolledIPSets() sets.Set[string] {
	pods := sets.New[string]()
	switch s.redirectMode {
	case IptablesMode:
		l, err := Ipset.List()
		if err != nil {
			log.Warnf("unable to list IPSet: %v", err)
		}
		for _, v := range l {
			pods.Insert(v.IP.String())
		}
	case EbpfMode:
		pods = s.ebpfServer.DumpAppIPs()
	}
	return pods
}

func addTProxyMarkRule() error {
	// Set up tproxy marks
	var rules []*netlink.Rule
	// TODO IPv6, append  unix.AF_INET6
	families := []int{unix.AF_INET}
	for _, family := range families {
		// Equiv: "ip rule add priority 20000 fwmark 0x400/0xfff lookup 100"
		tproxMarkRule := netlink.NewRule()
		tproxMarkRule.Family = family
		tproxMarkRule.Table = constants.RouteTableInbound
		tproxMarkRule.Mark = constants.TProxyMark
		tproxMarkRule.Mask = constants.TProxyMask
		tproxMarkRule.Priority = constants.TProxyMarkPriority
		rules = append(rules, tproxMarkRule)
	}

	for _, rule := range rules {
		log.Debugf("Adding netlink rule : %+v", rule)
		if err := netlink.RuleAdd(rule); err != nil {
			return fmt.Errorf("failed to configure netlink rule: %v", err)
		}
	}

	return nil
}

func addOrgSrcMarkRule() error {
	// Set up tproxy marks
	var rules []*netlink.Rule
	// TODO IPv6, append  unix.AF_INET6
	families := []int{unix.AF_INET}
	for _, family := range families {
		// Equiv: "ip rule add priority 20003 fwmark 0x4d3/0xfff lookup 100"
		orgSrcRule := netlink.NewRule()
		orgSrcRule.Family = family
		orgSrcRule.Table = constants.RouteTableInbound
		orgSrcRule.Mark = constants.OrgSrcRetMark
		orgSrcRule.Mask = constants.OrgSrcRetMask
		orgSrcRule.Priority = constants.OrgSrcPriority
		rules = append(rules, orgSrcRule)
	}

	for _, rule := range rules {
		log.Debugf("Adding netlink rule : %+v", rule)
		if err := netlink.RuleAdd(rule); err != nil {
			return fmt.Errorf("failed to configure netlink rule: %v", err)
		}
	}

	return nil
}

func disableRPFiltersForLink(ifaceName string) error {
	// Need to do some work in procfs
	// @TODO: This needs to be cleaned up, there are a lot of martians in AWS
	// that seem to necessitate this work and in theory we shouldn't *need* to disable
	// `rp_filter` with eBPF.
	//
	// 0 - No source validation.
	// 1 - Strict mode as defined in RFC3704 Strict Reverse Path
	// 2 - Loose mode as defined in RFC3704 Loose Reverse Path
	// Ideally we should be able to set it to 1 (strictest)
	procs := map[string]int{
		"/proc/sys/net/ipv4/conf/default/rp_filter":           0,
		"/proc/sys/net/ipv4/conf/all/rp_filter":               0,
		"/proc/sys/net/ipv4/conf/" + ifaceName + "/rp_filter": 0,
	}
	for proc, val := range procs {
		err := setProc(proc, fmt.Sprint(val))
		if err != nil {
			log.Errorf("failed to write to proc file %s: %v", proc, err)
			return err
		}
	}

	return nil
}

func setProc(path string, value string) error {
	return os.WriteFile(path, []byte(value), 0o644)
}

func buildRouteForPod(ip string) ([]string, error) {
	if ip == "" {
		return nil, errors.New("ip is none")
	}

	return []string{
		"table",
		fmt.Sprintf("%d", constants.RouteTableInbound),
		fmt.Sprintf("%s/32", ip),
		"via",
		constants.ZTunnelInboundTunIP,
		"dev",
		constants.InboundTun,
		"src",
		HostIP,
	}, nil
}

// This can be called on the node, as part of termination/cleanup,
// or it can be called from within a pod netns, as a "clean slate" prep.
func flushAllRouteTables() {
	// Clean up ip route tables
	_ = routeFlushTable(constants.RouteTableInbound)
	_ = routeFlushTable(constants.RouteTableOutbound)
	_ = routeFlushTable(constants.RouteTableProxy)
}

func routeFlushTable(table int) error {
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, &netlink.Route{Table: table}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return err
	}
	// default route is not handled proper in netlink
	// https://github.com/vishvananda/netlink/issues/670
	// https://github.com/vishvananda/netlink/issues/611
	for i, route := range routes {
		if (route.Dst == nil || route.Dst.IP == nil) && route.Src == nil && route.Gw == nil && route.MPLSDst == nil {
			_, defaultDst, _ := net.ParseCIDR("0.0.0.0/0")
			routes[i].Dst = defaultDst
		}
	}
	err = routesDelete(routes)
	if err != nil {
		return err
	}
	return nil
}

// This can be called on the node, as part of termination/cleanup,
// or it can be called from within a pod netns, as a "clean slate" prep.
//
// TODO `netlink.RuleDel` SHOULD work here - but it does not. Unsure why.
// So, for time being, rely on `ip`
func deleteIPRules(prioritiesToDelete []string, warnOnFail bool) {
	var exec []*ExecList
	for _, pri := range prioritiesToDelete {
		exec = append(exec, newExec("ip", []string{"rule", "del", "priority", pri}))
	}
	for _, e := range exec {
		err := execute(e.Cmd, e.Args...)
		if err != nil && warnOnFail {
			log.Warnf("Error running command %v %v: %v", e.Cmd, strings.Join(e.Args, " "), err)
		}
	}
}

// This can be called on the node, as part of termination/cleanup,
// or it can be called from within a pod netns, as a "clean slate" prep.
func deleteTunnelLinks(inboundName, outboundName string, warnOnFail bool) {
	// Delete geneve tunnel links

	// Re-fetch the container link to get its creation-time parameters, e.g. index and mac
	// Deleting by name doesn't work.
	inboundTun, err := netlink.LinkByName(inboundName)
	if err != nil && warnOnFail {
		log.Warnf("did not find existing inbound tunnel %s to delete: %v", inboundName, err)
	} else if inboundTun != nil {
		err = netlink.LinkSetDown(inboundTun)
		if err != nil {
			log.Warnf("failed to bring down inbound tunnel: %v", err)
		}
		err = netlink.LinkDel(inboundTun)
		if err != nil && warnOnFail {
			log.Warnf("error deleting inbound tunnel: %v", err)
		}
	}
	outboundTun, err := netlink.LinkByName(outboundName)
	if err != nil && warnOnFail {
		log.Warnf("did not find existing outbound tunnel %s to delete: %v", outboundName, err)
		// Bail, if we can't find it don't try to delete it
		return
	} else if outboundTun != nil {
		err = netlink.LinkSetDown(outboundTun)
		if err != nil {
			log.Warnf("failed to bring down outbound tunnel: %v", err)
		}
		err = netlink.LinkDel(outboundTun)
		if err != nil && warnOnFail {
			log.Warnf("error deleting outbound tunnel: %v", err)
		}
	}
}

func routesDelete(routes []netlink.Route) error {
	for _, r := range routes {
		err := netlink.RouteDel(&r)
		if err != nil {
			return err
		}
	}
	return nil
}
