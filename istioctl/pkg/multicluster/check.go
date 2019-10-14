// Copyright 2019 Istio Authors.
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
	indent                  = "    "
)

func CheckSingle(mesh *Mesh, cluster *KubeCluster, sortedClusters []*KubeCluster, env Environment) error {
	p := func(format string, a ...interface{}) {
		fmt.Fprintf(env.Stdout(), format+"\n", a...)
	}
	p("%v context=%v uid=%v network=%v istio=%v %v",
		strings.Repeat("-", 10),
		cluster.context,
		cluster.uid,
		cluster.Network,
		cluster.state.installed,
		strings.Repeat("-", 10))

	tw := tabwriter.NewWriter(env.Stdout(), 0, 8, 2, '\t', 0)
	fmt.Fprintf(tw, "%vNAME\tISSUER\tSUBJECT\tNOTAFTER\n", indent)
	for _, info := range []struct {
		cert *x509.Certificate
		name string
	}{
		{cluster.state.externalRootCert, "ExternalRootCert"},
		{cluster.state.externalCACert, "ExternalCACert"},
		{cluster.state.selfSignedRootCert, "SelfSignedRootCert"},
		{cluster.state.selfSignedCACert, "SelfSignedCACert"},
	} {
		if c := info.cert; c != nil {
			fmt.Fprintf(tw, "%v%v\t%q\t%q\t%v\n",
				indent,
				info.name,
				c.Issuer,
				c.Subject,
				c.NotAfter.Format(time.RFC3339))
		}
	}
	tw.Flush()

	fmt.Fprintln(env.Stdout())

	fmt.Fprintf(tw, "%vCONTEXT\tUID\tREGISTERED\tMASTER\t\n", indent)
	for _, other := range sortedClusters {
		if other.uid == cluster.uid {
			continue
		}

		synced := "N"
		server := "<UNKNOWN>"

		gotSecretName := other.uid
		if remoteSecret, ok := cluster.state.remoteSecrets[gotSecretName]; ok {
			synced = "Y"

			if kubeconfig, ok := remoteSecret.Data[other.uid]; ok {
				// gvk := api.SchemeGroupVersion.WithKind("Config")
				out, _, err := latest.Codec.Decode(kubeconfig, nil, nil)
				if err == nil {
					if config, ok := out.(*api.Config); ok {
						if cluster, ok := config.Clusters[config.CurrentContext]; ok {
							server = cluster.Server
						}
					}
				}
			}
		}

		if cluster, ok := env.GetConfig().Clusters[other.context]; ok {
			if server != cluster.Server {
				server += "(out-of-sync)"
			}
		}

		fmt.Fprintf(tw, "%v%v\t%v\t%v\t%v\n",
			indent,
			other.context,
			other.uid,
			synced,
			server,
		)
	}
	tw.Flush()

	//// TODO verify all clusters have common trust

	return nil
}

func Check(opt checkOptions, env Environment) error {
	mesh, err := meshFromFileDesc(opt.filename, opt.Kubeconfig, env)
	if err != nil {
		return err
	}

	if opt.all {
		for _, cluster := range mesh.sorted {
			if err := CheckSingle(mesh, cluster, mesh.sorted, env); err != nil {
				fmt.Fprintf(env.Stderr(), "could not check cluster %v: %v", cluster.context, err)
			}
			fmt.Fprintln(env.Stdout(), clusterDisplaySeparator)
		}
	} else {
		context := opt.Context
		if context == "" {
			context = env.GetConfig().CurrentContext
		}
		cluster, ok := mesh.clusters[context]
		if !ok {
			return fmt.Errorf("cluster %v not found", context)
		}
		return CheckSingle(mesh, cluster, mesh.sorted, env)
	}

	return nil
}

type checkOptions struct {
	KubeOptions
	filenameOption
	all bool
}

func (o *checkOptions) prepare(flags *pflag.FlagSet) error {
	o.KubeOptions.prepare(flags)
	return o.filenameOption.prepare()
}

func (o *checkOptions) addFlags(flags *pflag.FlagSet) {
	// o.KubeOptions.addFlags(flags)
	o.filenameOption.addFlags(flags)

	flags.BoolVar(&o.all, "all", o.all,
		"check the status of all clusters in the mesh")
}

func NewCheckCommand() *cobra.Command {
	opt := checkOptions{}
	c := &cobra.Command{
		Use:   "check",
		Short: `Check status of the multi-cluster mesh's control plane' `,
		RunE: func(c *cobra.Command, args []string) error {
			if err := opt.prepare(c.Flags()); err != nil {
				return err
			}
			env, err := newKubeEnvFromCobra(opt.Kubeconfig, opt.Context, c)
			if err != nil {
				return err
			}
			return Check(opt, env)
		},
	}
	opt.addFlags(c.PersistentFlags())
	return c
}
