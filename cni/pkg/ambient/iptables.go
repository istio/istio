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
	"strconv"
	"strings"

	"istio.io/istio/cni/pkg/ambient/constants"
)

type iptablesRule struct {
	Table    string
	Chain    string
	RuleSpec []string
}

// detectIptablesCommand will attempt to detect whether to use iptables-legacy, iptables or iptables-nft
// based on output of iptables-nft or if the command exists.
//
// Logic is based on Kubernetes https://github.com/danwinship/kubernetes/blob/ca32fd23cca0797aa787fc5d883807d4eee6899f/build/debian-iptables/iptables-wrapper
func (s *Server) detectIptablesCommand() string {
	var err error
	var numLegacyLines int
	var numNftLines int
	var output string

	log.Infof("Detecting iptables command")

	output, err = executeOutput("bash", "-c",
		"(iptables-legacy-save || true; ip6tables-legacy-save || true) 2>/dev/null | grep '^-' | wc -l",
	)
	if err != nil {
		log.Errorf("Error getting iptables-legacy-save output: %v, assuming 0", err)
	} else {
		numLegacyLines, err = strconv.Atoi(strings.TrimSpace(output))
		if err != nil {
			log.Errorf("Error converting iptables-legacy-save output to int: %v, assuming 0", err)
		}
	}

	if numLegacyLines > 10 {
		log.Infof("Detected iptables-legacy")
		return "iptables-legacy"
	}

	output, err = executeOutput("bash", "-c",
		`(timeout 5 sh -c "iptables-nft-save; ip6tables-nft-save" || true) 2>/dev/null | grep '^-' | wc -l`,
	)
	if err != nil {
		log.Errorf("Error getting iptables-nft-save output: %v, assuming 0", err)
	} else {
		numNftLines, err = strconv.Atoi(strings.TrimSpace(output))
		if err != nil {
			log.Errorf("Error converting iptables-nft-save output to int: %v, assuming 0", err)
		}
	}

	if numLegacyLines > numNftLines {
		log.Infof("Using iptables command: iptables-legacy")
		return "iptables-legacy"
	}
	log.Infof("Using iptables command: iptables-nft")
	return "iptables-nft"
}

func (s *Server) IptablesCmd() string {
	c, _ := s.iptablesCommand.Get()
	return c
}

// Initialize the chains and lists for ztunnel
func (s *Server) initializeLists() error {
	var err error

	list := []*ExecList{
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableNat, "-N", constants.ChainZTunnelPrerouting}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableNat, "-I", "PREROUTING", "-j", constants.ChainZTunnelPrerouting}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableNat, "-N", constants.ChainZTunnelPostrouting}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableNat, "-I", "POSTROUTING", "-j", constants.ChainZTunnelPostrouting}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableMangle, "-N", constants.ChainZTunnelPrerouting}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableMangle, "-I", "PREROUTING", "-j", constants.ChainZTunnelPrerouting}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableMangle, "-N", constants.ChainZTunnelPostrouting}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableMangle, "-I", "POSTROUTING", "-j", constants.ChainZTunnelPostrouting}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableMangle, "-N", constants.ChainZTunnelOutput}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableMangle, "-I", "OUTPUT", "-j", constants.ChainZTunnelOutput}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableMangle, "-N", constants.ChainZTunnelInput}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableMangle, "-I", "INPUT", "-j", constants.ChainZTunnelInput}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableMangle, "-N", constants.ChainZTunnelForward}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableMangle, "-I", "FORWARD", "-j", constants.ChainZTunnelForward}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableFilter, "-N", constants.ChainZTunnelForward}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableFilter, "-I", "FORWARD", "-j", constants.ChainZTunnelForward}),
	}

	for _, l := range list {
		err = execute(l.Cmd, l.Args...)
		if err != nil {
			if strings.Contains(err.Error(), "Chain already exists") {
				log.Debugf("Chain already exists caught during running command %v: %v", l.Cmd, err)
			} else {
				log.Errorf("Error running command %v (can safely ignore chain exist errors): %v", l.Cmd, err)
			}
		}
	}

	return nil
}

// Flush the chains and lists for ztunnel
// This method will log warnings if node already clean because chains
// are not created yet.
func (s *Server) flushLists() {
	var err error

	list := []*ExecList{
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableNat, "-F", constants.ChainZTunnelPrerouting}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableNat, "-F", constants.ChainZTunnelPostrouting}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableMangle, "-F", constants.ChainZTunnelPrerouting}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableMangle, "-F", constants.ChainZTunnelPostrouting}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableMangle, "-F", constants.ChainZTunnelOutput}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableMangle, "-F", constants.ChainZTunnelInput}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableMangle, "-F", constants.ChainZTunnelForward}),
		newExec(s.IptablesCmd(),
			[]string{"-t", constants.TableFilter, "-F", constants.ChainZTunnelForward}),
	}

	for _, l := range list {
		err = execute(l.Cmd, l.Args...)
		if err != nil {
			log.Warnf("Error running command %v: %v", l.Cmd, err)
		}
	}
}

// Clean the chains and lists for ztunnel
// This method will log warnings if node already clean because chains
// are not created yet.
func (s *Server) cleanRules() {
	var err error

	s.flushLists()

	list := []*ExecList{
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableNat,
				"-D", constants.ChainPrerouting,
				"-j", constants.ChainZTunnelPrerouting,
			},
		),
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableNat,
				"-X", constants.ChainZTunnelPrerouting,
			},
		),
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableNat,
				"-D", constants.ChainPostrouting,
				"-j", constants.ChainZTunnelPostrouting,
			},
		),
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableNat,
				"-X", constants.ChainZTunnelPostrouting,
			},
		),
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableMangle,
				"-D", constants.ChainPrerouting,
				"-j", constants.ChainZTunnelPrerouting,
			},
		),
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableMangle,
				"-X", constants.ChainZTunnelPrerouting,
			},
		),
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableMangle,
				"-D", constants.ChainPostrouting,
				"-j", constants.ChainZTunnelPostrouting,
			},
		),
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableMangle,
				"-X", constants.ChainZTunnelPostrouting,
			},
		),
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableMangle,
				"-D", constants.ChainForward,
				"-j", constants.ChainZTunnelForward,
			},
		),
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableMangle,
				"-X", constants.ChainZTunnelForward,
			},
		),
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableMangle,
				"-D", constants.ChainInput,
				"-j", constants.ChainZTunnelInput,
			},
		),
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableMangle,
				"-X", constants.ChainZTunnelInput,
			},
		),
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableMangle,
				"-D", constants.ChainOutput,
				"-j", constants.ChainZTunnelOutput,
			},
		),
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableMangle,
				"-X", constants.ChainZTunnelOutput,
			},
		),
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableFilter,
				"-D", constants.ChainForward,
				"-j", constants.ChainZTunnelForward,
			},
		),
		newExec(
			s.IptablesCmd(),
			[]string{
				"-t", constants.TableFilter,
				"-X", constants.ChainZTunnelForward,
			},
		),
	}

	for _, l := range list {
		err = execute(l.Cmd, l.Args...)
		if err != nil {
			log.Warnf("Error running command %v: %v", l.Cmd, err)
		}
	}
}

func newIptableRule(table, chain string, rule ...string) *iptablesRule {
	return &iptablesRule{
		Table:    table,
		Chain:    chain,
		RuleSpec: rule,
	}
}

func (s *Server) iptablesAppend(rules []*iptablesRule) error {
	for _, rule := range rules {
		log.Debugf("Appending rule: %+v", rule)
		err := execute(s.IptablesCmd(), append([]string{"-t", rule.Table, "-A", rule.Chain}, rule.RuleSpec...)...)
		if err != nil {
			return err
		}
	}
	return nil
}
