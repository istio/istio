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

var IptablesCmd = "iptables-nft"

// DetectIptablesCommand will attempt to detect whether to use iptables-legacy, iptables or iptables-nft
// based on output of iptables-nft or if the command exists.
//
// Logic is based on Kubernetes https://github.com/danwinship/kubernetes/blob/ca32fd23cca0797aa787fc5d883807d4eee6899f/build/debian-iptables/iptables-wrapper
func (s *Server) DetectIptablesCommand() {
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
		IptablesCmd = "iptables-legacy"
		log.Infof("Detected iptables-legacy")
		return
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
		IptablesCmd = "iptables-legacy"
	} else {
		IptablesCmd = "iptables-nft"
	}

	log.Infof("Using iptables command: %s", IptablesCmd)
}

// Initialize the chains and lists for uproxy
// https://github.com/solo-io/istio-sidecarless/blob/master/redirect-worker.sh#L36-L47
func (s *Server) initializeLists() error {
	var err error

	s.DetectIptablesCommand()

	list := []*ExecList{
		newExec(IptablesCmd,
			[]string{"-t", constants.TableNat, "-N", constants.ChainUproxyPrerouting}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableNat, "-I", "PREROUTING", "-j", constants.ChainUproxyPrerouting}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableNat, "-N", constants.ChainUproxyPostrouting}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableNat, "-I", "POSTROUTING", "-j", constants.ChainUproxyPostrouting}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableMangle, "-N", constants.ChainUproxyPrerouting}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableMangle, "-I", "PREROUTING", "-j", constants.ChainUproxyPrerouting}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableMangle, "-N", constants.ChainUproxyPostrouting}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableMangle, "-I", "POSTROUTING", "-j", constants.ChainUproxyPostrouting}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableMangle, "-N", constants.ChainUproxyOutput}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableMangle, "-I", "OUTPUT", "-j", constants.ChainUproxyOutput}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableMangle, "-N", constants.ChainUproxyInput}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableMangle, "-I", "INPUT", "-j", constants.ChainUproxyInput}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableMangle, "-N", constants.ChainUproxyForward}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableMangle, "-I", "FORWARD", "-j", constants.ChainUproxyForward}),
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

// Flush the chains and lists for uproxy
// https://github.com/solo-io/istio-sidecarless/blob/master/redirect-worker.sh#L29-L34
func (s *Server) flushLists() {
	var err error

	list := []*ExecList{
		newExec(IptablesCmd,
			[]string{"-t", constants.TableNat, "-F", constants.ChainUproxyPrerouting}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableNat, "-F", constants.ChainUproxyPostrouting}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableMangle, "-F", constants.ChainUproxyPrerouting}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableMangle, "-F", constants.ChainUproxyPostrouting}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableMangle, "-F", constants.ChainUproxyOutput}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableMangle, "-F", constants.ChainUproxyInput}),
		newExec(IptablesCmd,
			[]string{"-t", constants.TableMangle, "-F", constants.ChainUproxyForward}),
	}

	for _, l := range list {
		err = execute(l.Cmd, l.Args...)
		if err != nil {
			log.Warnf("Error running command %v: %v", l.Cmd, err)
		}
	}
}

func (s *Server) cleanRules() {
	var err error

	s.flushLists()

	list := []*ExecList{
		newExec(
			IptablesCmd,
			[]string{
				"-t", constants.TableNat,
				"-D", constants.ChainPrerouting,
				"-j", constants.ChainUproxyPrerouting,
			},
		),
		newExec(
			IptablesCmd,
			[]string{
				"-t", constants.TableNat,
				"-X", constants.ChainUproxyPrerouting,
			},
		),
		newExec(
			IptablesCmd,
			[]string{
				"-t", constants.TableNat,
				"-D", constants.ChainPostrouting,
				"-j", constants.ChainUproxyPostrouting,
			},
		),
		newExec(
			IptablesCmd,
			[]string{
				"-t", constants.TableNat,
				"-X", constants.ChainUproxyPostrouting,
			},
		),
		newExec(
			IptablesCmd,
			[]string{
				"-t", constants.TableMangle,
				"-D", constants.ChainPrerouting,
				"-j", constants.ChainUproxyPrerouting,
			},
		),
		newExec(
			IptablesCmd,
			[]string{
				"-t", constants.TableMangle,
				"-X", constants.ChainUproxyPrerouting,
			},
		),
		newExec(
			IptablesCmd,
			[]string{
				"-t", constants.TableMangle,
				"-D", constants.ChainPostrouting,
				"-j", constants.ChainUproxyPostrouting,
			},
		),
		newExec(
			IptablesCmd,
			[]string{
				"-t", constants.TableMangle,
				"-X", constants.ChainUproxyPostrouting,
			},
		),
		newExec(
			IptablesCmd,
			[]string{
				"-t", constants.TableMangle,
				"-D", constants.ChainForward,
				"-j", constants.ChainUproxyForward,
			},
		),
		newExec(
			IptablesCmd,
			[]string{
				"-t", constants.TableMangle,
				"-X", constants.ChainUproxyForward,
			},
		),
		newExec(
			IptablesCmd,
			[]string{
				"-t", constants.TableMangle,
				"-D", constants.ChainInput,
				"-j", constants.ChainUproxyInput,
			},
		),
		newExec(
			IptablesCmd,
			[]string{
				"-t", constants.TableMangle,
				"-X", constants.ChainUproxyInput,
			},
		),
		newExec(
			IptablesCmd,
			[]string{
				"-t", constants.TableMangle,
				"-D", constants.ChainOutput,
				"-j", constants.ChainUproxyOutput,
			},
		),
		newExec(
			IptablesCmd,
			[]string{
				"-t", constants.TableMangle,
				"-X", constants.ChainUproxyOutput,
			},
		),
	}

	for _, l := range list {
		err = execute(l.Cmd, l.Args...)
		if err != nil {
			log.Errorf("Error running command %v: %v", l.Cmd, err)
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

func iptablesAppend(rules []*iptablesRule) error {
	for _, rule := range rules {
		log.Debugf("Appending rule: %+v", rule)
		err := execute(IptablesCmd, append([]string{"-t", rule.Table, "-A", rule.Chain}, rule.RuleSpec...)...)
		if err != nil {
			return err
		}
	}
	return nil
}
