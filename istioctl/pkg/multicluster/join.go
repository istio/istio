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
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"istio.io/istio/pkg/kube/secretcontroller"
)

func Join(opt joinOptions, env Environment) error {
	mesh, err := meshFromFileDesc(opt.filename, opt.Kubeconfig, env)
	if err != nil {
		return err
	}

	if opt.all {
		for _, cluster := range mesh.sortedClusters {
			if err := joinServiceRegistries(mesh, env); err != nil {
				env.Errorf("error: could not join cluster %v to mesh: %v\n", cluster, err)
			}
		}
	} else {
		if err := joinServiceRegistries(mesh, env); err != nil {
			return err
		}
	}

	return nil
}

func deleteSecret(cluster *Cluster, s *v1.Secret) error {
	return cluster.client.CoreV1().Secrets(cluster.Namespace).Delete(s.Name, &metav1.DeleteOptions{})
}

func applySecret(cluster *Cluster, curr *v1.Secret) error {
	err := wait.Poll(500*time.Millisecond, 5*time.Second, func() (bool, error) {
		prev, err := cluster.client.CoreV1().Secrets(cluster.Namespace).Get(curr.Name, metav1.GetOptions{})
		if err == nil {
			prev.StringData = curr.StringData
			prev.Annotations[clusterContextAnnotationKey] = cluster.context
			prev.Labels[secretcontroller.MultiClusterSecretLabel] = "true"
			if _, err := cluster.client.CoreV1().Secrets(cluster.Namespace).Update(prev); err != nil {
				return false, err
			}
		} else {
			if _, err := cluster.client.CoreV1().Secrets(cluster.Namespace).Create(curr); err != nil {
				return false, err
			}
		}
		return true, nil
	})
	return err
}

func joinServiceRegistries(mesh *Mesh, env Environment) error {
	preparedSecrets := make(map[string]*v1.Secret)
	pruneCandidates := make(map[string]map[string]*v1.Secret)

	for _, cluster := range mesh.sortedClusters {
		fmt.Printf("creating secret for first %v\n", cluster)
		// skip clustersByContext without Istio installed
		if !cluster.installed {
			continue
		}

		opt := RemoteSecretOptions{
			KubeOptions: KubeOptions{
				Context: cluster.context,
			},
			ServiceAccountName: cluster.ServiceAccountReader,
			// TODO add auth provider option (e.g. gcp)
		}
		secret, err := createRemoteSecret(opt, env)
		if err != nil {
			return fmt.Errorf("%v: %v", cluster.context, err)
		}

		preparedSecrets[cluster.uid] = secret

		// build the list of preparedSecrets to potentially pruneCandidates from this first
		pruneCandidates[cluster.context] = cluster.readRemoteSecrets(env)
	}

	for _, first := range mesh.sortedClusters {
		for _, second := range mesh.sortedClusters {
			if first.uid == second.uid {
				continue
			}

			env.Printf("Joining %v and %v\n", first, second)

			// pairwise Join
			for _, s := range []struct {
				local  *Cluster
				remote *Cluster
			}{
				{first, second},
				{second, first},
			} {
				remoteSecret, ok := preparedSecrets[s.local.uid]
				if !ok {
					continue
				}

				if err := applySecret(s.local, remoteSecret); err != nil {
					env.Errorf("%v failed: %v\n", s.local, err)
				} else {
					env.Printf("%v registered with %v\n", s.remote, s.local)
				}

				delete(pruneCandidates[s.remote.context], remoteSecret.Name)
			}
		}
	}

	// pruneCandidates any leftover preparedSecrets
	for context, secrets := range pruneCandidates {
		for _, secret := range secrets {
			fmt.Printf("pruning secret  %v from first %v\n", secret.Name, context)
			if err := deleteSecret(mesh.clustersByContext[context], secret); err != nil {
				return err
			}
		}
	}

	return nil
}

type joinOptions struct {
	KubeOptions
	filenameOption

	trust            bool
	serviceDiscovery bool
	all              bool
}

func (o *joinOptions) prepare(flags *pflag.FlagSet) error {
	o.KubeOptions.prepare(flags)
	return o.filenameOption.prepare()
}

func (o *joinOptions) addFlags(flags *pflag.FlagSet) {
	o.filenameOption.addFlags(flags)

	flags.BoolVar(&o.trust, "trust", true,
		"establish trust between clustersByContext in the mesh")
	flags.BoolVar(&o.serviceDiscovery, "discovery", true,
		"link Istio service discovery with the clustersByContext service registriesS")
	flags.BoolVar(&o.all, "all", o.all,
		"join all clustersByContext together in the mesh")
}

func NewJoinCommand() *cobra.Command {
	opt := joinOptions{}
	c := &cobra.Command{
		Use:   "join",
		Short: `Join multiple clustersByContext into a single multi-cluster mesh`,
		RunE: func(c *cobra.Command, args []string) error {
			if err := opt.prepare(c.Flags()); err != nil {
				return err
			}
			env, err := NewEnvironmentFromCobra(opt.Kubeconfig, opt.Context, c)
			if err != nil {
				return err
			}
			return Join(opt, env)
		},
	}
	opt.addFlags(c.PersistentFlags())
	return c
}

/*
TODO
* unit tests
* automatic analyze-style check; consider writing an analyzer for this later.
* create passthrough gateway
* add egress gateway option for non-auth multi-network case.
* foreach helper
*/
