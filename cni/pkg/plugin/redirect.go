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

// Defines the redirect object and operations.
package plugin

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/cmd/pilot-agent/options"
	"istio.io/istio/tools/istio-iptables/pkg/cmd"
	"istio.io/pkg/log"
)

const (
	redirectModeREDIRECT         = "REDIRECT"
	redirectModeTPROXY           = "TPROXY"
	defaultProxyStatusPort       = "15020"
	defaultRedirectToPort        = "15001"
	defaultNoRedirectUID         = "1337"
	defaultRedirectMode          = redirectModeREDIRECT
	defaultRedirectIPCidr        = "*"
	defaultRedirectExcludeIPCidr = ""
	defaultRedirectExcludePort   = defaultProxyStatusPort
	defaultKubevirtInterfaces    = ""
	defaultIncludeInboundPorts   = "*"
	defaultIncludeOutboundPorts  = ""
)

var (
	includeIPCidrsKey       = annotation.SidecarTrafficIncludeOutboundIPRanges.Name
	excludeIPCidrsKey       = annotation.SidecarTrafficExcludeOutboundIPRanges.Name
	excludeInboundPortsKey  = annotation.SidecarTrafficExcludeInboundPorts.Name
	includeInboundPortsKey  = annotation.SidecarTrafficIncludeInboundPorts.Name
	excludeOutboundPortsKey = annotation.SidecarTrafficExcludeOutboundPorts.Name
	includeOutboundPortsKey = annotation.SidecarTrafficIncludeOutboundPorts.Name

	sidecarInterceptModeKey = annotation.SidecarInterceptionMode.Name
	sidecarPortListKey      = annotation.SidecarStatusPort.Name

	kubevirtInterfacesKey = annotation.SidecarTrafficKubevirtInterfaces.Name

	annotationRegistry = map[string]*annotationParam{
		"inject":               {injectAnnotationKey, "", alwaysValidFunc},
		"status":               {sidecarStatusKey, "", alwaysValidFunc},
		"redirectMode":         {sidecarInterceptModeKey, defaultRedirectMode, validateInterceptionMode},
		"ports":                {sidecarPortListKey, "", validatePortList},
		"includeIPCidrs":       {includeIPCidrsKey, defaultRedirectIPCidr, validateCIDRListWithWildcard},
		"excludeIPCidrs":       {excludeIPCidrsKey, defaultRedirectExcludeIPCidr, validateCIDRList},
		"excludeInboundPorts":  {excludeInboundPortsKey, defaultRedirectExcludePort, validatePortListWithWildcard},
		"includeInboundPorts":  {includeInboundPortsKey, defaultIncludeInboundPorts, validatePortListWithWildcard},
		"excludeOutboundPorts": {excludeOutboundPortsKey, defaultRedirectExcludePort, validatePortListWithWildcard},
		"includeOutboundPorts": {includeOutboundPortsKey, defaultIncludeOutboundPorts, validatePortListWithWildcard},
		"kubevirtInterfaces":   {kubevirtInterfacesKey, defaultKubevirtInterfaces, alwaysValidFunc},
	}
)

// Redirect -- the istio-cni redirect object
type Redirect struct {
	targetPort           string
	redirectMode         string
	noRedirectUID        string
	includeIPCidrs       string
	excludeIPCidrs       string
	excludeInboundPorts  string
	excludeOutboundPorts string
	includeInboundPorts  string
	includeOutboundPorts string
	kubevirtInterfaces   string
	excludeInterfaces    string
	dnsRedirect          bool
	invalidDrop          bool
	hostNSEnterExec      bool
}

type annotationValidationFunc func(value string) error

type annotationParam struct {
	key        string
	defaultVal string
	validator  annotationValidationFunc
}

func alwaysValidFunc(value string) error {
	return nil
}

// validateInterceptionMode validates the interceptionMode annotation
func validateInterceptionMode(mode string) error {
	switch mode {
	case redirectModeREDIRECT:
	case redirectModeTPROXY:
	default:
		return fmt.Errorf("interceptionMode invalid: %v", mode)
	}
	return nil
}

func validateCIDRList(cidrs string) error {
	if len(cidrs) > 0 {
		for _, cidr := range strings.Split(cidrs, ",") {
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				return fmt.Errorf("failed parsing cidr '%s': %v", cidr, err)
			}
		}
	}
	return nil
}

func splitPorts(portsString string) []string {
	return strings.Split(portsString, ",")
}

func dedupPorts(ports []string) []string {
	dedup := make(map[string]bool)
	keys := []string{}

	for _, port := range ports {
		if !dedup[port] {
			dedup[port] = true
			keys = append(keys, port)
		}
	}
	return keys
}

func parsePort(portStr string) (uint16, error) {
	port, err := strconv.ParseUint(strings.TrimSpace(portStr), 10, 16)
	if err != nil {
		return 0, fmt.Errorf("failed parsing port %q: %v", portStr, err)
	}
	return uint16(port), nil
}

func parsePorts(portsString string) ([]int, error) {
	portsString = strings.TrimSpace(portsString)
	ports := make([]int, 0)
	if len(portsString) > 0 {
		for _, portStr := range splitPorts(portsString) {
			port, err := parsePort(portStr)
			if err != nil {
				return nil, err
			}
			ports = append(ports, int(port))
		}
	}
	return ports, nil
}

func validatePortList(ports string) error {
	if _, err := parsePorts(ports); err != nil {
		return fmt.Errorf("portList %q invalid: %v", ports, err)
	}
	return nil
}

func validatePortListWithWildcard(ports string) error {
	if ports != "*" {
		return validatePortList(ports)
	}
	return nil
}

// ValidateIncludeIPRanges validates the includeIPRanges parameter
func validateCIDRListWithWildcard(ipRanges string) error {
	if ipRanges != "*" {
		if e := validateCIDRList(ipRanges); e != nil {
			return fmt.Errorf("IPRanges invalid: %v", e)
		}
	}
	return nil
}

func getAnnotationOrDefault(name string, annotations map[string]string) (isFound bool, val string, err error) {
	if _, ok := annotationRegistry[name]; !ok {
		return false, "", fmt.Errorf("no registered annotation with name=%s", name)
	}
	// use annotation value if present
	if val, found := annotations[annotationRegistry[name].key]; found {
		if err := annotationRegistry[name].validator(val); err != nil {
			return true, annotationRegistry[name].defaultVal, err
		}
		return true, val, nil
	}
	// no annotation found so use default value
	return false, annotationRegistry[name].defaultVal, nil
}

// NewRedirect returns a new Redirect Object constructed from a list of ports and annotations
func NewRedirect(pi *PodInfo) (*Redirect, error) {
	var isFound bool
	var valErr error

	redir := &Redirect{}
	redir.targetPort = defaultRedirectToPort
	isFound, redir.redirectMode, valErr = getAnnotationOrDefault("redirectMode", pi.Annotations)
	if valErr != nil {
		return nil, fmt.Errorf("annotation value error for value %s; annotationFound = %t: %v",
			"redirectMode", isFound, valErr)
	}
	redir.noRedirectUID = defaultNoRedirectUID
	isFound, redir.includeIPCidrs, valErr = getAnnotationOrDefault("includeIPCidrs", pi.Annotations)
	if valErr != nil {
		return nil, fmt.Errorf("annotation value error for value %s; annotationFound = %t: %v",
			"includeIPCidrs", isFound, valErr)
	}
	isFound, redir.excludeIPCidrs, valErr = getAnnotationOrDefault("excludeIPCidrs", pi.Annotations)
	if valErr != nil {
		return nil, fmt.Errorf("annotation value error for value %s; annotationFound = %t: %v",
			"excludeIPCidrs", isFound, valErr)
	}
	isFound, redir.excludeInboundPorts, valErr = getAnnotationOrDefault("excludeInboundPorts", pi.Annotations)
	if valErr != nil {
		return nil, fmt.Errorf("annotation value error for value %s; annotationFound = %t: %v",
			"excludeInboundPorts", isFound, valErr)
	}
	isFound, redir.includeInboundPorts, valErr = getAnnotationOrDefault("includeInboundPorts", pi.Annotations)
	if valErr != nil {
		return nil, fmt.Errorf("annotation value error for value %s; annotationFound = %t: %v",
			"includeInboundPorts", isFound, valErr)
	}
	isFound, redir.excludeOutboundPorts, valErr = getAnnotationOrDefault("excludeOutboundPorts", pi.Annotations)
	if valErr != nil {
		return nil, fmt.Errorf("annotation value error for value %s; annotationFound = %t: %v",
			"excludeOutboundPorts", isFound, valErr)
	}
	isFound, redir.includeOutboundPorts, valErr = getAnnotationOrDefault("includeOutboundPorts", pi.Annotations)
	if valErr != nil {
		return nil, fmt.Errorf("annotation value error for value %s; annotationFound = %t: %v",
			"includeOutboundPorts", isFound, valErr)
	}
	// Add 15090 to sync with non-cni injection template
	// TODO: Revert below once https://github.com/istio/istio/pull/23037 or its follow up is merged.
	redir.excludeInboundPorts = strings.TrimSpace(redir.excludeInboundPorts)
	if len(redir.excludeInboundPorts) > 0 && redir.excludeInboundPorts[len(redir.excludeInboundPorts)-1] != ',' {
		redir.excludeInboundPorts += ","
	}
	redir.excludeInboundPorts += "15020,15021,15090"
	redir.excludeInboundPorts = strings.Join(dedupPorts(splitPorts(redir.excludeInboundPorts)), ",")
	isFound, redir.kubevirtInterfaces, valErr = getAnnotationOrDefault("kubevirtInterfaces", pi.Annotations)
	if valErr != nil {
		return nil, fmt.Errorf("annotation value error for value %s; annotationFound = %t: %v",
			"kubevirtInterfaces", isFound, valErr)
	}
	if v, found := pi.ProxyEnvironments[options.DNSCaptureByAgent.Name]; found {
		// parse and set the bool value of dnsRedirect
		redir.dnsRedirect, valErr = strconv.ParseBool(v)
		if valErr != nil {
			log.Warnf("cannot parse DNS capture environment variable %v", valErr)
		}
	}
	if v, found := pi.ProxyEnvironments[cmd.InvalidDropByIptables.Name]; found {
		// parse and set the bool value of invalidDrop
		redir.invalidDrop, valErr = strconv.ParseBool(v)
		if valErr != nil {
			log.Warnf("cannot parse invalid drop environment variable %v", valErr)
		}
	}
	return redir, nil
}
