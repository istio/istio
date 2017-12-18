// Copyright 2017 Istio Authors.
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

package controlplane

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

const (
	defaultTag = "0.4.0"
)

// Command returns the "control-plane" subcommand for istioctl.
func Command(istioNamespaceFlag *string) *cobra.Command {
	var (
		features          *[]string
		out               string
		helmChartLocation string
	)

	install := defaultInstall()
	cmd := &cobra.Command{
		Use:   "control-plane",
		Short: "Generates the configuration for Istio's control plane.",
		Long: "istioctl control-plane produces deployment files to run the minimum Istio control for the set of " +
			"features requested by the --feature flag. If no features are provided, we create deployments for the " +
			"default control plane: Pilot, Mixer, CA, and Ingress Proxies, with mTLS enabled.",
		Example: `istioctl control-plane --features routing,policy,initializer -o helm`,
		RunE: func(c *cobra.Command, args []string) error {
			if err := install.setFeatures(*features); err != nil {
				return err
			}
			install.Namespace = *istioNamespaceFlag
			switch strings.ToLower(out) {
			case "helm":
				_, err := fmt.Fprint(os.Stdout, valuesFromInstallation(install))
				return err
			case "yaml":
				rendered, err := yamlFromInstallation(install, helmChartLocation)
				if err != nil {
					return err
				}
				_, err = fmt.Fprint(os.Stdout, rendered)
				return err
			default:
				return fmt.Errorf("unsupported output %q", out)
			}
		},
	}

	features = cmd.PersistentFlags().StringArrayP("features", "f", []string{},
		`List of Istio features to enable. Accepts any combination of "mtls", "telemetry", "routing", "ingress", "policy", "initializer".`)
	cmd.PersistentFlags().StringVar(&install.Hub, "hub", "gcr.io/istio-testing", "The container registry to pull Istio images from")
	cmd.PersistentFlags().StringVar(&install.MixerTag, "mixer-tag", defaultTag, "The tag to use to pull the `mixer` container")
	cmd.PersistentFlags().StringVar(&install.PilotTag, "pilot-tag", defaultTag, "The tag to use to pull the `pilot-discovery` container")
	cmd.PersistentFlags().StringVar(&install.CaTag, "ca-tag", defaultTag, "The tag to use to pull the `ca` container")
	cmd.PersistentFlags().StringVar(&install.ProxyTag, "proxy-tag", defaultTag, "The tag to use to pull the `proxy` container")
	cmd.PersistentFlags().BoolVar(&install.Debug, "debug", false, "If true, uses debug images instead of release images")
	cmd.PersistentFlags().Uint16Var(&install.NodePort, "ingress-node-port", 0,
		"If provided, Istio ingress proxies will run as a NodePort service mapped to the port provided by this flag. "+
			"Note that this flag is ignored unless the \"ingress\" feature flag is provided too.")
	cmd.PersistentFlags().StringVarP(&out, "out", "o", "helm", `Output format. Acceptable values are:
					"helm": produces contents of values.yaml
					"yaml": produces Kubernetes deployments`)

	// TODO: figure out how we want to package up the charts with the binary to make this easy
	cmd.PersistentFlags().StringVar(&helmChartLocation, "helm-chart-dir", ".",
		"The directory to find the helm charts used to render Istio deployments. -o yaml uses these to render the helm chart locally.")

	_ = cmd.PersistentFlags().MarkHidden("hub")
	_ = cmd.PersistentFlags().MarkHidden("mixer-tag")
	_ = cmd.PersistentFlags().MarkHidden("pilot-tag")
	_ = cmd.PersistentFlags().MarkHidden("ca-tag")
	_ = cmd.PersistentFlags().MarkHidden("proxy-tag")
	return cmd
}

type installation struct {
	Mixer       bool
	Pilot       bool
	Ca          bool
	Ingress     bool
	Initializer bool

	Namespace string
	Debug     bool
	NodePort  uint16

	// todo: support hub per component
	Hub      string // hub to pull images from
	MixerTag string
	PilotTag string
	CaTag    string
	ProxyTag string
}

func defaultInstall() *installation {
	return &installation{
		Mixer:   true,
		Pilot:   true,
		Ca:      true,
		Ingress: true,
	}
}

func (i *installation) setFeatures(features []string) error {
	if len(features) == 0 {
		return nil
	}

	i.Mixer = false
	i.Pilot = false
	i.Ca = false
	i.Ingress = false
	i.Initializer = false
	for _, f := range features {
		switch strings.ToLower(f) {
		case "telemetry", "policy":
			i.Mixer = true
			i.Pilot = true
		case "routing":
			i.Pilot = true
		case "mtls":
			i.Ca = true
			i.Pilot = true
		case "ingress":
			i.Ingress = true
			i.Pilot = true
		case "initializer":
			i.Initializer = true
		default:
			return fmt.Errorf("invalid feature name %q", f)
		}
	}
	return nil
}
