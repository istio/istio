// Copyright Istio Authors.
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

package multicluster

import (
	"crypto/x509"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/clientcmd/api/latest"
)

var (
	clusterDisplaySeparator = strings.Repeat("-", 60)
)

type remoteSecretStatus string

const (
	rsStatusNotFound          remoteSecretStatus = "notFound"
	rsStatusConfigMissing     remoteSecretStatus = "configMissing"
	rsStatusConfigDecodeError remoteSecretStatus = "configDecodeError"
	rsStatusConfigInvalid     remoteSecretStatus = "configInvalid"
	rsStatusServerNotFound    remoteSecretStatus = "serverNotFound"
	rsStatusOk                remoteSecretStatus = "ok"
)

func secretStateAndServer(srs remoteSecrets, c *Cluster) (remoteSecretStatus, string) {
	remoteSecret, ok := srs[c.Name]
	if !ok {
		return rsStatusNotFound, ""
	}
	key := c.Name
	kubeconfig, ok := remoteSecret.Data[key]
	if !ok {
		return rsStatusConfigMissing, ""
	}
	out, _, err := latest.Codec.Decode(kubeconfig, nil, nil)
	if err != nil {
		return rsStatusConfigDecodeError, ""
	}
	config, ok := out.(*api.Config)
	if !ok {
		return rsStatusConfigInvalid, ""
	}
	cluster, ok := config.Clusters[config.CurrentContext]
	if !ok {
		return rsStatusServerNotFound, ""
	}

	return rsStatusOk, cluster.Server
}

func describeCACerts(printer Printer, c *Cluster, indent string) {
	secrets := c.readCACerts(printer)

	tw := tabwriter.NewWriter(printer.Stdout(), 0, 8, 2, '\t', 0)
	_, _ = fmt.Fprintf(tw, "%vNAME\tISSUER\tSUBJECT\tNOTAFTER\n", indent)

	for _, info := range []struct {
		cert *x509.Certificate
		name string
	}{
		{secrets.externalRootCert, "ExternalRootCert"},
		{secrets.externalCACert, "ExternalCACert"},
		{secrets.selfSignedCACert, "SelfSignedRootCert"},
	} {
		if c := info.cert; c != nil {
			_, _ = fmt.Fprintf(tw, "%v%v\t%q\t%q\t%v\n",
				indent,
				info.name,
				c.Issuer,
				c.Subject,
				c.NotAfter.Format(time.RFC3339))
		}
	}

	_ = tw.Flush()
}

func describeRemoteSecrets(printer Printer, mesh *Mesh, c *Cluster, indent string) {
	serviceRegistrySecrets := c.readRemoteSecrets(printer)

	tw := tabwriter.NewWriter(printer.Stdout(), 0, 8, 2, '\t', 0)
	_, _ = fmt.Fprintf(tw, "%vCONTEXT\tUID\tREGISTERED\tMASTER\t\n", indent)
	for _, other := range mesh.SortedClusters() {
		if other.Name == c.Name {
			continue
		}

		secretState, server := secretStateAndServer(serviceRegistrySecrets, other)

		_, _ = fmt.Fprintf(tw, "%v%v\t%v\t%v\t%v\n",
			indent,
			other.Context,
			other.Name,
			secretState,
			server,
		)
	}
	_ = tw.Flush()
}

func describeIngressGateways(printer Printer, c *Cluster, indent string) { // nolint: interfacer
	gateways := c.readIngressGateways()

	printer.Printf("%vgateways: ", indent)
	if len(gateways) == 0 {
		printer.Printf("<none>")
	} else {
		for i, gateway := range gateways {
			printer.Printf("%v", gateway)
			if i < len(gateways)-1 {
				printer.Printf(", ")
			}
		}
	}
	printer.Printf("\n")
}

func describeCluster(printer Printer, mesh *Mesh, c *Cluster) error {
	printer.Printf("%v Context=%v clusterName=%v network=%v istio=%v %v\n",
		strings.Repeat("-", 10),
		c.Context,
		c.Name,
		c.Network,
		c.Installed,
		strings.Repeat("-", 10))

	indent := strings.Repeat(" ", 4)

	printer.Printf("\n")
	describeCACerts(printer, c, indent)

	printer.Printf("\n")
	describeRemoteSecrets(printer, mesh, c, indent)

	printer.Printf("\n")
	describeIngressGateways(printer, c, indent)

	// TODO verify all clustersByContext have common trust

	return nil
}

// Describe status of the multi-cluster mesh's control plane.
func Describe(all bool, mesh *Mesh, printer Printer) error {
	if all {
		for _, cluster := range mesh.SortedClusters() {
			if err := describeCluster(printer, mesh, cluster); err != nil {
				printer.Errorf("could not describe cluster %v: %v\n", cluster, err)
			}
			printer.Printf("%v\n", clusterDisplaySeparator)
		}
		return nil
	}

	return describeCluster(printer, mesh, mesh.Subject())
}

type describeOptions struct {
	KubeOptions
	filenameOption
	all bool
}

func (o *describeOptions) prepare(flags *pflag.FlagSet) error {
	o.KubeOptions.prepare(flags)
	return o.filenameOption.prepare()
}

func (o *describeOptions) addFlags(flags *pflag.FlagSet) {
	o.filenameOption.addFlags(flags)

	flags.BoolVar(&o.all, "all", true,
		"describe the status of all clustersByContext in the mesh")
}

func NewDescribeCommand() *cobra.Command {
	opt := describeOptions{
		all: false,
	}
	c := &cobra.Command{
		Use:   "describe -f <mesh.yaml> [--all]",
		Short: `Describe status of the multi-cluster mesh's control plane' `,
		RunE: func(c *cobra.Command, args []string) error {
			if err := opt.prepare(c.Flags()); err != nil {
				return err
			}

			printer := NewPrinterFromCobra(c)
			clientFactory := NewClientFactory()

			kubeContext, err := contextOrDefault(opt.Kubeconfig, opt.Context)
			if err != nil {
				return err
			}

			mesh, err := meshFromFileDesc(opt.filename, opt.Kubeconfig, kubeContext, clientFactory, printer)
			if err != nil {
				return err
			}

			return Describe(opt.all, mesh, printer)
		},
	}
	opt.addFlags(c.PersistentFlags())
	return c
}
