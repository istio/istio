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
	rsStatusNotFound           remoteSecretStatus = "notFound"
	rsStatusConfigMissing      remoteSecretStatus = "configMissing"
	rsStatusConfigDecodeError  remoteSecretStatus = "configDecodeError"
	rsStatusConfigInvalid      remoteSecretStatus = "configInvalid"
	rsStatusServerNotFound     remoteSecretStatus = "serverNotFound"
	seStatusServerAddrMismatch remoteSecretStatus = "serverAddrMismatch"
	rsStatusOk                 remoteSecretStatus = "ok"
)

func secretStateAndServer(env Environment, srs remoteSecrets, c *Cluster) (remoteSecretStatus, string) {
	remoteSecret, ok := srs[c.clusterName]
	if !ok {
		return rsStatusNotFound, ""
	}
	key := c.clusterName
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

	server := cluster.Server
	if configCluster, ok := env.GetConfig().Clusters[c.Context]; ok {
		if server != configCluster.Server {
			return seStatusServerAddrMismatch, fmt.Sprintf("%v (%v from local kubeconfig)", server, configCluster.Server)
		}
	}

	return rsStatusOk, cluster.Server
}

func describeCACerts(env Environment, c *Cluster, indent string) {
	secrets := c.readCACerts(env)

	tw := tabwriter.NewWriter(env.Stdout(), 0, 8, 2, '\t', 0)
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

func describeRemoteSecrets(env Environment, mesh *Mesh, c *Cluster, indent string) {
	serviceRegistrySecrets := c.readRemoteSecrets(env)

	tw := tabwriter.NewWriter(env.Stdout(), 0, 8, 2, '\t', 0)
	_, _ = fmt.Fprintf(tw, "%vCONTEXT\tUID\tREGISTERED\tMASTER\t\n", indent)
	for _, other := range mesh.SortedClusters() {
		if other.clusterName == c.clusterName {
			continue
		}

		secretState, server := secretStateAndServer(env, serviceRegistrySecrets, other)

		_, _ = fmt.Fprintf(tw, "%v%v\t%v\t%v\t%v\n",
			indent,
			other.Context,
			other.clusterName,
			secretState,
			server,
		)
	}
	_ = tw.Flush()
}

func describeIngressGateways(env Environment, c *Cluster, indent string) { // nolint: interfacer
	gateways := c.readIngressGateways()

	env.Printf("%vgateways: ", indent)
	if len(gateways) == 0 {
		env.Printf("<none>")
	} else {
		for i, gateway := range gateways {
			env.Printf("%v", gateway)
			if i < len(gateways)-1 {
				env.Printf(", ")
			}
		}
	}
	env.Printf("\n")
}

func describeCluster(env Environment, mesh *Mesh, c *Cluster) error {
	env.Printf("%v Context=%v clusterName=%v network=%v istio=%v %v\n",
		strings.Repeat("-", 10),
		c.Context,
		c.clusterName,
		c.Network,
		c.installed,
		strings.Repeat("-", 10))

	indent := strings.Repeat(" ", 4)

	env.Printf("\n")
	describeCACerts(env, c, indent)

	env.Printf("\n")
	describeRemoteSecrets(env, mesh, c, indent)

	env.Printf("\n")
	describeIngressGateways(env, c, indent)

	// TODO verify all clustersByContext have common trust

	return nil
}

func Describe(opt describeOptions, env Environment) error {
	mesh, err := meshFromFileDesc(opt.filename, env)
	if err != nil {
		return err
	}

	if opt.all {
		for _, cluster := range mesh.SortedClusters() {
			if err := describeCluster(env, mesh, cluster); err != nil {
				env.Errorf("could not describe cluster %v: %v\n", cluster, err)
			}
			env.Printf("%v\n", clusterDisplaySeparator)
		}
	} else {
		context := opt.Context
		if context == "" {
			context = env.GetConfig().CurrentContext
		}
		cluster, ok := mesh.clustersByContext[context]
		if !ok {
			return fmt.Errorf("cluster %v not found", context)
		}
		return describeCluster(env, mesh, cluster)
	}

	return nil
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
			env, err := NewEnvironmentFromCobra(opt.Kubeconfig, opt.Context, c)
			if err != nil {
				return err
			}
			return Describe(opt, env)
		},
	}
	opt.addFlags(c.PersistentFlags())
	return c
}
