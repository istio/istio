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
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/kube/secretcontroller"
)

func deleteSecret(cluster *Cluster, s *v1.Secret) error {
	return cluster.client.CoreV1().Secrets(cluster.Namespace).Delete(s.Name, &metav1.DeleteOptions{})
}

// update current state to match desired state.
func updateRemoteSecret(prev, curr *v1.Secret) (changed bool) {
	prev.StringData = curr.StringData
	for k, v := range curr.StringData {
		newVal := []byte(v)
		if !bytes.Equal(prev.Data[k], newVal) {
			prev.Data[k] = newVal
			changed = true
		}
	}

	if prev.Annotations[clusterContextAnnotationKey] != curr.Annotations[clusterContextAnnotationKey] {
		prev.Annotations[clusterContextAnnotationKey] = curr.Annotations[clusterContextAnnotationKey]
		changed = true
	}

	if prev.Labels[secretcontroller.MultiClusterSecretLabel] != "true" {
		prev.Labels[secretcontroller.MultiClusterSecretLabel] = "true"
		changed = true
	}

	return changed
}

func applySecret(env Environment, cluster *Cluster, curr *v1.Secret) error {
	err := env.Poll(500*time.Millisecond, 5*time.Second, func() (bool, error) {
		prev, err := cluster.client.CoreV1().Secrets(cluster.Namespace).Get(curr.Name, metav1.GetOptions{})
		if err == nil {
			if changed := updateRemoteSecret(prev, curr); changed {
				if _, err := cluster.client.CoreV1().Secrets(cluster.Namespace).Update(prev); err != nil {
					return false, err
				}
			}
			return true, nil
		}

		if _, err := cluster.client.CoreV1().Secrets(cluster.Namespace).Create(curr); err != nil {
			return false, err
		}
		return true, nil
	})
	return err
}

func apply(mesh *Mesh, env Environment) error {
	var errs *multierror.Error

	currentSecretsByUID := make(map[types.UID]*v1.Secret)
	existingSecretsByUID := make(map[types.UID]map[types.UID]*v1.Secret)

	sortedClusters := mesh.SortedClusters()
	for _, cluster := range sortedClusters {
		// skip clusters without Istio installed
		if !cluster.installed {
			env.Printf("not joining cluster %v, Istio control plane not found\n", cluster)
			continue
		}

		opt := RemoteSecretOptions{
			KubeOptions: KubeOptions{
				Context:   cluster.Context,
				Namespace: cluster.Namespace,
			},
			ServiceAccountName: cluster.ServiceAccountReader,
			AuthType:           RemoteSecretAuthTypeBearerToken,
			// TODO add auth provider option (e.g. gcp)
		}
		secret, err := createRemoteSecret(opt, cluster.client, env)
		if err != nil {
			err := fmt.Errorf("not joining cluster %v, could not creating remote secret: %v", cluster.Context, err)
			errs = multierror.Append(errs, err)
			continue
		}

		currentSecretsByUID[cluster.uid] = secret

		// build the list of currentSecretsByUID to potentially prune
		existingSecretsByUID[cluster.uid] = cluster.readRemoteSecrets(env)
	}

	joined := make(map[string]bool)

	for _, first := range sortedClusters {
		if first.DisableRegistryJoin || !first.installed {
			continue
		}

		for _, second := range sortedClusters {
			if first.uid == second.uid {
				continue
			}

			if second.DisableRegistryJoin || !second.installed {
				continue
			}

			// skip pairs we've already joined
			id0, id1 := string(first.uid), string(second.uid)
			if strings.Compare(id0, id1) > 0 {
				id1, id0 = id0, id1
			}
			hash := id0 + "/" + id1
			if _, ok := joined[hash]; ok {
				continue
			}
			joined[hash] = true

			env.Printf("(re)joining %v and %v\n", first, second)

			// pairwise join
			for _, s := range []struct {
				local  *Cluster
				remote *Cluster
			}{
				{first, second},
				{second, first},
			} {
				remoteSecret, ok := currentSecretsByUID[s.remote.uid]
				if !ok {
					continue
				}

				if err := applySecret(env, s.local, remoteSecret); err != nil {
					env.Errorf("%v failed: %v\n", s.local, err)
				}
				delete(existingSecretsByUID[s.local.uid], s.remote.uid)
			}
		}
	}

	// existingSecretsByUID any leftover currentSecretsByUID
	for uid, secrets := range existingSecretsByUID {
		for _, secret := range secrets {
			cluster := mesh.clustersByUID[uid]
			fmt.Printf("Pruning %v from %v\n", secret.Name, cluster)
			if err := deleteSecret(cluster, secret); err != nil {
				err := fmt.Errorf("failed to prune secret %v from cluster %v: %v", secret.Name, cluster, err)
				env.Errorf(err.Error())
				errs = multierror.Append(errs, err)
				continue
			}
		}
	}

	return errs.ErrorOrNil()
}

type applyOptions struct {
	KubeOptions
	filenameOption
}

func (o *applyOptions) prepare(flags *pflag.FlagSet) error {
	o.KubeOptions.prepare(flags)
	return o.filenameOption.prepare()
}

func (o *applyOptions) addFlags(flags *pflag.FlagSet) {
	o.filenameOption.addFlags(flags)
}

// NewApplyCommand creates a new command for applying multicluster configuration to the mesh.
func NewApplyCommand() *cobra.Command {
	opt := applyOptions{}
	c := &cobra.Command{
		Use:   "apply  -f <mesh.yaml>",
		Short: `Update clusters in a multi-cluster mesh based on mesh topology`,
		RunE: func(c *cobra.Command, args []string) error {
			if err := opt.prepare(c.Flags()); err != nil {
				return err
			}
			env, err := NewEnvironmentFromCobra(opt.Kubeconfig, opt.Context, c)
			if err != nil {
				return err
			}
			mesh, err := meshFromFileDesc(opt.filename, env)
			if err != nil {
				return err
			}
			return apply(mesh, env)
		},
	}
	opt.addFlags(c.PersistentFlags())
	return c
}
