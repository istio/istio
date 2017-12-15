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

// Command returns the "control-plane" subcommand for istioctl.
func Command(istioNamespaceFlag *string) *cobra.Command {
	var features *[]string
	var out string

	m := defaultModel()
	cmd := &cobra.Command{
		Use:   "control-plane",
		Short: "Generates the configuration for Istio's control plane.",
		Long: "istioctl control-plane produces deployment files to run the minimum Istio control for the set of " +
			"features requested by the --feature flag. If no features are provided, we create deployments for the " +
			"default control plane: Pilot, Mixer, CA, and Ingress Proxies, with mTLS enabled.",
		Example: `istioctl control-plane --features routing,policy,initializer -o helm`,
		RunE: func(c *cobra.Command, args []string) error {
			m.setFeatures(*features)
			m.Namespace = *istioNamespaceFlag
			switch strings.ToLower(out) {
			case "helm":
				_, err := fmt.Fprint(os.Stdout, fromModel(m))
				return err
			case "yaml":
				return fmt.Errorf("not implemented")
			default:
				return fmt.Errorf("unsupported output %q", out)
			}
		},
	}

	features = cmd.PersistentFlags().StringArrayP("features", "f", []string{},
		`List of Istio features to enable. Accepts any combination of "mtls", "telemetry", "routing", "ingress", "policy", "initializer".`)
	cmd.PersistentFlags().StringVar(&m.Hub, "hub", "gcr.io/istio-testing", "The container registry to pull Istio images from")
	cmd.PersistentFlags().StringVar(&m.MixerTag, "mixer-tag", "latest", "The tag to use to pull the `mixer` container")
	cmd.PersistentFlags().StringVar(&m.PilotTag, "pilot-tag", "latest", "The tag to use to pull the `pilot-discovery` container")
	cmd.PersistentFlags().StringVar(&m.CaTag, "ca-tag", "latest", "The tag to use to pull the `ca` container")
	cmd.PersistentFlags().StringVar(&m.ProxyTag, "proxy-tag", "latest", "The tag to use to pull the `proxy` container")
	cmd.PersistentFlags().BoolVar(&m.Debug, "debug", false, "If true, uses debug images instead of release images")
	cmd.PersistentFlags().Uint16Var(&m.NodePort, "ingress-node-port", 0,
		"If provided, Istio ingress proxies will run as a NodePort service mapped to the port provided by this flag. "+
			"Note that this flag is ignored unless the \"ingress\" feature flag is provided too")
	cmd.PersistentFlags().StringVarP(&out, "out", "o", "helm", `Output format. Acceptable values are:
					"helm": produces contents of values.yaml
					"yaml": produces Kubernetes deployments`)

	return cmd
}

type model struct {
	Mixer       bool
	Pilot       bool
	Ca          bool
	Ingress     bool
	Initializer bool

	Namespace string
	// todo: support hub per component
	Hub      string
	MixerTag string
	PilotTag string
	CaTag    string
	ProxyTag string
	Debug    bool
	NodePort uint16
}

func defaultModel() *model {
	return &model{
		Mixer:   true,
		Pilot:   true,
		Ca:      true,
		Ingress: true,
	}
}

func (m *model) setFeatures(features []string) error {
	if len(features) == 0 {
		return nil
	}

	m.Mixer = false
	m.Pilot = false
	m.Ca = false
	m.Ingress = false
	m.Initializer = false
	for _, f := range features {
		switch strings.ToLower(f) {
		case "telemetry", "policy":
			m.Mixer = true
		case "routing":
			m.Pilot = true
		case "mtls":
			m.Ca = true
			m.Pilot = true
		case "ingress":
			m.Ingress = true
			m.Pilot = true
		case "initializer":
			m.Initializer = true
		default:
			return fmt.Errorf("invalid feature name %q", f)
		}
	}
	return nil
}
