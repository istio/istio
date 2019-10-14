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
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"

	"istio.io/istio/pkg/kube/secretcontroller"
)

func Join(opt joinOptions, env Environment) error {
	mesh, err := meshFromFileDesc(opt.filename, opt.Kubeconfig, env)
	if err != nil {
		return err
	}

	if opt.all {
		for _, cluster := range mesh.sorted {
			if err := joinServiceRegistries(mesh, env); err != nil {
				fmt.Fprintf(env.Stderr(), "could not join cluster %v to mesh: %v", cluster.context, err)
			}
		}
	} else {
		if err := joinServiceRegistries(mesh, env); err != nil {
			return err
		}
	}

	return nil
}

func deleteSecret(cluster *KubeCluster, s *v1.Secret) error {
	return cluster.client.CoreV1().Secrets(cluster.Namespace).Delete(s.Name, &metav1.DeleteOptions{})
}

func applySecret(cluster *KubeCluster, curr *v1.Secret) error {
	err := wait.Poll(500*time.Millisecond, 5*time.Second, func() (bool, error) {
		prev, err := cluster.client.CoreV1().Secrets(cluster.Namespace).Get(curr.Name, metav1.GetOptions{})
		if err == nil {
			prev.StringData = curr.StringData
			prev.Annotations[clusterContextAnnotationKey] = cluster.context
			prev.Labels[secretcontroller.MultiClusterSecretLabel] = "true"
			prev.Labels[managedSecretLabel] = "true"
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
	secrets := make(map[string]*v1.Secret)
	prune := make(map[string]map[string]*v1.Secret)

	// generate secrets
	for _, cluster := range mesh.sorted {
		fmt.Printf("creating secret for first %v\n", cluster.context)
		// skip clusters without Istio installed
		if !cluster.state.installed {
			continue
		}
		context := cluster.context

		// TODO add auth provider option (e.g. gcp)
		tokenSecret, err := getServiceAccountSecretToken(cluster.client, cluster.ServiceAccountReader, cluster.Namespace)
		if err != nil {
			return fmt.Errorf("%v: %v", context, err)
		}

		_, server, err := getCurrentContextAndClusterServerFromKubeconfig(cluster.context, env.GetConfig())
		if err != nil {
			return fmt.Errorf("%v: %v", context, err)
		}

		secret, err := createRemoteSecretFromTokenAndServer(tokenSecret, cluster.uid, context, server)
		if err != nil {
			return fmt.Errorf("%v: %v", context, err)
		}
		secret.Labels[managedSecretLabel] = "true"
		secrets[cluster.uid] = secret

		// build the list of secrets to potentially prune from this first
		existing, err := cluster.client.CoreV1().Secrets(cluster.Namespace).List(metav1.ListOptions{
			FieldSelector: fields.SelectorFromSet(fields.Set{managedSecretLabel: "true"}).String(),
		})
		if err == nil {
			for _, secret := range existing.Items {
				if _, ok := prune[context]; !ok {
					prune[context] = make(map[string]*v1.Secret)
				}
				prune[context][secret.Name] = &secret
			}
		}
	}

	for _, first := range mesh.sorted {
		for _, second := range mesh.sorted {
			if first.uid == second.uid {
				continue
			}

			fmt.Fprintf(env.Stdout(), "Joining %v (%v) and %v (%v)\n",
				first.uid, first.context, second.uid, second.context)

			// pairwise Join
			for _, s := range []struct {
				local  *KubeCluster
				remote *KubeCluster
			}{
				{first, second},
				{second, first},
			} {
				remoteSecret, ok := secrets[s.local.uid]
				if !ok {
					continue
				}

				if err := applySecret(s.local, remoteSecret); err != nil {
					fmt.Fprintf(env.Stderr(), "%v failed: %v", s.local.context, err)
				} else {
					fmt.Fprintf(env.Stdout(), "%v registered with %v", s.remote.context, s.local.context)
				}

				delete(prune[s.remote.context], remoteSecret.Name)
			}
		}
	}

	// prune any leftover secrets
	for context, secrets := range prune {
		for _, secret := range secrets {
			fmt.Printf("pruning secret  %v from first %v\n", secret.Name, context)
			if err := deleteSecret(mesh.clusters[context], secret); err != nil {
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
		"establish trust between clusters in the mesh")
	flags.BoolVar(&o.serviceDiscovery, "discovery", true,
		"link Istio service discovery with the clusters service registriesS")
	flags.BoolVar(&o.all, "all", o.all,
		"join all clusters together in the mesh")
}

func NewJoinCommand() *cobra.Command {
	opt := joinOptions{}
	c := &cobra.Command{
		Use:   "join",
		Short: `Join multiple clusters into a single multi-cluster mesh`,
		RunE: func(c *cobra.Command, args []string) error {
			if err := opt.prepare(c.Flags()); err != nil {
				return err
			}
			env, err := newKubeEnvFromCobra(opt.Kubeconfig, opt.Context, c)
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
