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

package gendeployment

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

const (
	defaultTag          = "release-1.0-latest-daily"
	defaultHyperkubeTag = "v1.7.6_coreos.0"
)

// Command returns the "gen-deploy" subcommand for istioctl.
func Command(istioNamespaceFlag *string) *cobra.Command {
	var (
		helmChartLocation string
		valuesPath        string
	)

	install := defaultInstall()
	cmd := &cobra.Command{
		Use:     "gen-deploy",
		Short:   "Generates the configuration for Istio's control plane.",
		Long:    "istioctl gen-deploy produces deployment files to run the Istio.",
		Example: `istioctl gen-deploy --values myvalues.yaml`,
		RunE: func(c *cobra.Command, args []string) error {
			install.Namespace = *istioNamespaceFlag
			// TODO: this is NOT merged with the values.yaml from helm directory.

			values, err := getValues(valuesPath, install)
			if err != nil {
				return err
			}
			rendered, err := yamlFromInstallation(values, *istioNamespaceFlag, helmChartLocation)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(os.Stdout, rendered)
			return err
		},
	}

	cmd.PersistentFlags().StringVar(&valuesPath, "values", "", "Path to the Helm values.yaml file used to render YAML "+
		"deployments locally when --out=yaml. Flag values are ignored in favor of using the file directly.")

	cmd.PersistentFlags().StringVar(&install.Hub, "hub", install.Hub, "The container registry to pull Istio images from")
	cmd.PersistentFlags().StringVar(&install.MixerTag, "mixer-tag", install.MixerTag, "The tag to use to pull the `mixer` container")
	cmd.PersistentFlags().StringVar(&install.PilotTag, "pilot-tag", install.PilotTag, "The tag to use to pull the `pilot-discovery` container")
	cmd.PersistentFlags().StringVar(&install.CaTag, "ca-tag", install.CaTag, "The tag to use to pull the `ca` container")
	cmd.PersistentFlags().StringVar(&install.ProxyTag, "proxy-tag", install.ProxyTag, "The tag to use to pull the `proxy` container")
	cmd.PersistentFlags().BoolVar(&install.Debug, "debug", install.Debug, "If true, uses debug images instead of release images")
	cmd.PersistentFlags().Uint16Var(&install.NodePort, "ingress-node-port", install.NodePort,
		"If provided, Istio ingress proxies will run as a NodePort service mapped to the port provided by this flag. "+
			"Note that this flag is ignored unless the \"ingress\" feature flag is provided too.")

	// TODO: figure out how we want to package up the charts with the binary to make this easy
	cmd.PersistentFlags().StringVar(&helmChartLocation, "helm-chart-dir", ".",
		"The directory to find the helm charts used to render Istio deployments. -o yaml uses these to render the helm chart locally.")

	cmd.PersistentFlags().StringVar(&install.HyperkubeHub, "hyperkube-hub", install.HyperkubeHub, "The container registry to pull Hyperkube images from")
	cmd.PersistentFlags().StringVar(&install.HyperkubeTag, "hyperkube-tag", install.HyperkubeTag, "The tag to use to pull the `Hyperkube` container")

	_ = cmd.PersistentFlags().MarkHidden("hub")
	_ = cmd.PersistentFlags().MarkHidden("mixer-tag")
	_ = cmd.PersistentFlags().MarkHidden("pilot-tag")
	_ = cmd.PersistentFlags().MarkHidden("ca-tag")
	_ = cmd.PersistentFlags().MarkHidden("proxy-tag")
	return cmd
}

func getValues(path string, i *installation) (string, error) {
	if path == "" {
		return "", nil
	}

	out, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type installation struct {
	Namespace string

	// todo: support hub per component
	Hub      string // hub to pull images from
	MixerTag string
	PilotTag string
	CaTag    string
	ProxyTag string

	HyperkubeHub string
	HyperkubeTag string

	NodePort uint16
	Debug    bool

	Mixer           bool
	Pilot           bool
	CA              bool
	Ingress         bool
	SidecarInjector bool
}

func defaultInstall() *installation {
	return &installation{
		Mixer:           true,
		Pilot:           true,
		CA:              true,
		Ingress:         true,
		SidecarInjector: false,

		Namespace: "istio-system",
		Debug:     false,
		NodePort:  0,

		Hub:      "gcr.io/istio-release",
		MixerTag: defaultTag,
		PilotTag: defaultTag,
		CaTag:    defaultTag,
		ProxyTag: defaultTag,

		HyperkubeHub: "quay.io/coreos/hyperkube",
		HyperkubeTag: defaultHyperkubeTag,
	}
}

func (i *installation) setFeatures(features []string) error {
	if len(features) == 0 {
		return nil
	} else if len(features) == 1 {
		features = strings.Split(features[0], ",")
	}

	i.Mixer = false
	i.Pilot = false
	i.CA = false
	i.Ingress = false
	i.SidecarInjector = false
	for _, f := range features {
		switch strings.ToLower(f) {
		case "telemetry", "policy":
			i.Mixer = true
			i.Pilot = true
		case "routing":
			i.Pilot = true
		case "mtls":
			i.CA = true
			i.Pilot = true
		case "ingress":
			i.Ingress = true
			i.Pilot = true
		case "sidecar-injector":
			i.SidecarInjector = true
		default:
			return fmt.Errorf("invalid feature name %q", f)
		}
	}
	return nil
}
