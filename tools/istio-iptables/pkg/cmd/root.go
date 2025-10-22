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

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"istio.io/istio/pkg/flag"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tools/common/config"
	"istio.io/istio/tools/common/tproxy"
	"istio.io/istio/tools/istio-iptables/pkg/capture"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
	"istio.io/istio/tools/istio-iptables/pkg/validation"
	nftables "istio.io/istio/tools/istio-nftables/pkg/nft"
)

const InvalidDropByIptables = "INVALID_DROP"

func handleErrorWithCode(err error, code int) {
	log.Error(err)
	os.Exit(code)
}

func bindCmdlineFlags(cfg *config.Config, cmd *cobra.Command) {
	fs := cmd.Flags()
	flag.Bind(fs, constants.EnvoyPort, "p", "Specify the envoy port to which redirect all TCP traffic.", &cfg.ProxyPort)

	flag.BindEnv(fs, constants.InboundCapturePort, "z",
		"Port to which all inbound TCP traffic to the pod/VM should be redirected to.",
		&cfg.InboundCapturePort)

	flag.BindEnv(fs, constants.InboundTunnelPort, "e",
		"Specify the istio tunnel port for inbound tcp traffic.",
		&cfg.InboundTunnelPort)

	flag.BindEnv(fs, constants.ProxyUID, "u",
		"Specify the UID of the user for which the redirection is not applied. Typically, this is the UID of the proxy container.",
		&cfg.ProxyUID)

	flag.BindEnv(fs, constants.ProxyGID, "g",
		"Specify the GID of the user for which the redirection is not applied (same default value as -u param).",
		&cfg.ProxyGID)

	flag.BindEnv(fs, constants.InboundInterceptionMode, "m",
		"The mode used to redirect inbound connections to Envoy, either \"REDIRECT\" or \"TPROXY\".",
		&cfg.InboundInterceptionMode)

	flag.BindEnv(fs, constants.InboundPorts, "b",
		"Comma separated list of inbound ports for which traffic is to be redirected to Envoy (optional). "+
			"The wildcard character \"*\" can be used to configure redirection for all ports. An empty list will disable.",
		&cfg.InboundPortsInclude)

	flag.BindEnv(fs, constants.LocalExcludePorts, "d",
		"Comma separated list of inbound ports to be excluded from redirection to Envoy (optional). "+
			"Only applies when all inbound traffic (i.e. \"*\") is being redirected.",
		&cfg.InboundPortsExclude)

	flag.BindEnv(fs, constants.ExcludeInterfaces, "c",
		"Comma separated list of NIC (optional). Neither inbound nor outbound traffic will be captured.",
		&cfg.ExcludeInterfaces)

	flag.BindEnv(fs, constants.ServiceCidr, "i",
		"Comma separated list of IP ranges in CIDR form to redirect to envoy (optional). "+
			"The wildcard character \"*\" can be used to redirect all outbound traffic. An empty list will disable all outbound.",
		&cfg.OutboundIPRangesInclude)

	flag.BindEnv(fs, constants.ServiceExcludeCidr, "x",
		"Comma separated list of IP ranges in CIDR form to be excluded from redirection. "+
			"Only applies when all  outbound traffic (i.e. \"*\") is being redirected.",
		&cfg.OutboundIPRangesExclude)

	flag.BindEnv(fs, constants.OutboundPorts, "q",
		"Comma separated list of outbound ports to be explicitly included for redirection to Envoy.",
		&cfg.OutboundPortsInclude)

	flag.BindEnv(fs, constants.LocalOutboundPortsExclude, "o",
		"Comma separated list of outbound ports to be excluded from redirection to Envoy.",
		&cfg.OutboundPortsExclude)

	flag.BindEnv(fs, constants.RerouteVirtualInterfaces, "k",
		"Comma separated list of virtual interfaces whose inbound traffic (from VM) will be treated as outbound.",
		&cfg.RerouteVirtualInterfaces)

	flag.BindEnv(fs, constants.InboundTProxyMark, "t", "", &cfg.InboundTProxyMark)

	flag.BindEnv(fs, constants.InboundTProxyRouteTable, "r", "", &cfg.InboundTProxyRouteTable)

	flag.BindEnv(fs, constants.DryRun, "n", "Do not call any external dependencies like iptables.",
		&cfg.DryRun)

	flag.BindEnv(fs, constants.IptablesProbePort, "", "Set listen port for failure detection.", &cfg.IptablesProbePort)

	flag.BindEnv(fs, constants.ProbeTimeout, "", "Failure detection timeout.", &cfg.ProbeTimeout)

	flag.BindEnv(fs, constants.SkipRuleApply, "", "Skip iptables apply.", &cfg.SkipRuleApply)

	flag.BindEnv(fs, constants.RunValidation, "", "Validate iptables.", &cfg.RunValidation)

	flag.BindEnv(fs, constants.RedirectDNS, "", "Enable capture of dns traffic by istio-agent.", &cfg.RedirectDNS)
	// Allow binding to a different var, for consistency with other components
	flag.AdditionalEnv(fs, constants.RedirectDNS, "ISTIO_META_DNS_CAPTURE")

	flag.BindEnv(fs, constants.DropInvalid, "", "Enable invalid drop in the iptables rules.", &cfg.DropInvalid)
	// This could have just used the default but for backwards compat we support the old env.
	flag.AdditionalEnv(fs, constants.DropInvalid, InvalidDropByIptables)

	flag.BindEnv(fs, constants.DualStack, "", "Enable ipv4/ipv6 redirects for dual-stack.", &cfg.DualStack)
	// Allow binding to a different var, for consistency with other components
	flag.AdditionalEnv(fs, constants.DualStack, "ISTIO_DUAL_STACK")

	flag.BindEnv(fs, constants.CaptureAllDNS, "",
		"Instead of only capturing DNS traffic to DNS server IP, capture all DNS traffic at port 53. This setting is only effective when redirect dns is enabled.",
		&cfg.CaptureAllDNS)

	flag.BindEnv(fs, constants.NetworkNamespace, "", "The network namespace that iptables rules should be applied to.",
		&cfg.NetworkNamespace)

	flag.BindEnv(fs, constants.CNIMode, "", "Whether to run as CNI plugin.", &cfg.HostFilesystemPodNetwork)

	flag.BindEnv(fs, constants.Reconcile, "", "Reconcile pre-existing and incompatible iptables rules instead of failing if drift is detected.",
		&cfg.Reconcile)

	flag.BindEnv(fs, constants.CleanupOnly, "", "Perform a forced cleanup without creating new iptables chains or rules.",
		&cfg.CleanupOnly)

	// This flag is a safety measure in case the idempotency changes of #50328 backfire.
	// Allow bypassing of iptables idempotency handling, and attempts to apply iptables rules regardless of table state, which may cause unrecoverable failures.
	// Consider removing it after several releases with no reported issues.
	flag.BindEnv(fs, constants.ForceApply, "", "Apply iptables changes even if they appear to already be in place.",
		&cfg.ForceApply)

	// This mode is an alternative for iptables. It uses nftables rules for traffic redirection.
	flag.BindEnv(fs, constants.NativeNftables, "", "Use native nftables instead of iptables rules.",
		&cfg.NativeNftables)

	// This allows the user to specify the iptables version to use.
	flag.BindEnv(fs, constants.ForceIptablesBinary, "", "Break glass option to choose which iptables binary to use.",
		&cfg.ForceIptablesBinary)
}

func GetCommand(logOpts *log.Options) *cobra.Command {
	cfg := config.DefaultConfig()
	cmd := &cobra.Command{
		Use:   "istio-iptables",
		Short: "Set up iptables rules for Istio Sidecar",
		Long:  "istio-iptables is responsible for setting up port forwarding for Istio Sidecar.",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := log.Configure(logOpts); err != nil {
				return err
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			if err := cfg.FillConfigFromEnvironment(); err != nil {
				handleErrorWithCode(err, 1)
			}
			if err := cfg.Validate(); err != nil {
				handleErrorWithCode(err, 1)
			}
			runMethod := ProgramIptables

			// If nftables is enabled, use nft rules for traffic redirection.
			if cfg.NativeNftables {
				runMethod = nftables.ProgramNftables
			}

			if err := runMethod(cfg); err != nil {
				handleErrorWithCode(err, 1)
			}

			if cfg.RunValidation {
				validator := validation.NewValidator(cfg)

				if err := validator.Run(); err != nil {
					// nolint: revive, stylecheck
					msg := fmt.Errorf(`iptables validation failed; workload is not ready for Istio.
When using Istio CNI, this can occur if a pod is scheduled before the node is ready.

If installed with 'cni.repair.deletePods=true', this pod should automatically be deleted and retry.
Otherwise, this pod will need to be manually removed so that it is scheduled on a node with istio-cni running, allowing iptables rules to be established.
`)
					handleErrorWithCode(msg, constants.ValidationErrorCode)
				}
			}
		},
	}
	bindCmdlineFlags(cfg, cmd)
	return cmd
}

type IptablesError struct {
	Error    error
	ExitCode int
}

func ProgramIptables(cfg *config.Config) error {
	var ext dep.Dependencies
	if cfg.DryRun {
		log.Info("running iptables in dry-run mode, no rule changes will be made")
		ext = &dep.DependenciesStub{}
	} else {
		ext = &dep.RealDependencies{
			UsePodScopedXtablesLock: cfg.HostFilesystemPodNetwork,
			NetworkNamespace:        cfg.NetworkNamespace,
			ForceIptablesBinary:     cfg.ForceIptablesBinary,
		}
	}

	if !cfg.SkipRuleApply {
		iptConfigurator, err := capture.NewIptablesConfigurator(cfg, ext)
		if err != nil {
			return err
		}
		if err := iptConfigurator.Run(); err != nil {
			return err
		}
		if err := tproxy.ConfigureRoutes(cfg); err != nil {
			return fmt.Errorf("failed to configure routes: %v", err)
		}
	}
	return nil
}
