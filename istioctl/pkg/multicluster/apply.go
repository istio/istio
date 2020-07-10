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
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube/secretcontroller"
)

func deleteSecret(cluster *Cluster, s *v1.Secret) error {
	return cluster.CoreV1().Secrets(cluster.Namespace).Delete(context.TODO(), s.Name, metav1.DeleteOptions{})
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

	if prev.Annotations[clusterNameAnnotationKey] != curr.Annotations[clusterNameAnnotationKey] {
		prev.Annotations[clusterNameAnnotationKey] = curr.Annotations[clusterNameAnnotationKey]
		changed = true
	}

	if prev.Labels[secretcontroller.MultiClusterSecretLabel] != "true" {
		prev.Labels[secretcontroller.MultiClusterSecretLabel] = "true"
		changed = true
	}

	return changed
}

func applySecret(poller Poller, cluster *Cluster, curr *v1.Secret) error {
	err := poller.Poll(500*time.Millisecond, 5*time.Second, func() (bool, error) {
		prev, err := cluster.CoreV1().Secrets(cluster.Namespace).Get(context.TODO(), curr.Name, metav1.GetOptions{})
		if err == nil {
			if changed := updateRemoteSecret(prev, curr); changed {
				if _, err := cluster.CoreV1().Secrets(cluster.Namespace).Update(context.TODO(),
					prev, metav1.UpdateOptions{}); err != nil {
					return false, err
				}
			}
			return true, nil
		}

		if _, err := cluster.CoreV1().Secrets(cluster.Namespace).Create(context.TODO(), curr, metav1.CreateOptions{}); err != nil {
			return false, err
		}
		return true, nil
	})
	return err
}

// Apply updates clusters in a multi-cluster mesh based on mesh topology.
func Apply(mesh *Mesh, poller Poller, printer Printer) error {
	var errs *multierror.Error

	currentSecretsByUID := make(map[string]*v1.Secret)
	existingSecretsByUID := make(map[string]map[string]*v1.Secret)

	sortedClusters := mesh.SortedClusters()
	for _, cluster := range sortedClusters {
		// skip clusters without Istio installed
		if !cluster.Installed {
			printer.Printf("not joining cluster %v, Istio control plane not found\n", cluster)
			continue
		}

		// TODO add auth provider option (e.g. gcp)
		secret, err := CreateRemoteSecret(cluster.Name, cluster.Namespace, cluster.ServiceAccountReader,
			RemoteSecretAuthTypeBearerToken, "", nil, cluster)
		if err != nil {
			err := fmt.Errorf("not joining cluster %v, could not creating remote secret: %v", cluster.Context, err)
			errs = multierror.Append(errs, err)
			continue
		}

		currentSecretsByUID[cluster.Name] = secret

		// build the list of currentSecretsByUID to potentially prune
		existingSecretsByUID[cluster.Name] = cluster.readRemoteSecrets(printer)
	}

	joined := make(map[string]bool)

	for _, first := range sortedClusters {
		if first.DisableRegistryJoin || !first.Installed {
			continue
		}

		for _, second := range sortedClusters {
			if first.Name == second.Name {
				continue
			}

			if second.DisableRegistryJoin || !second.Installed {
				continue
			}

			// skip pairs we've already joined
			id0, id1 := first.Name, second.Name
			if strings.Compare(id0, id1) > 0 {
				id1, id0 = id0, id1
			}
			hash := id0 + "/" + id1
			if _, ok := joined[hash]; ok {
				continue
			}
			joined[hash] = true

			printer.Printf("(re)joining %v and %v\n", first, second)

			// pairwise join
			for _, s := range []struct {
				local  *Cluster
				remote *Cluster
			}{
				{first, second},
				{second, first},
			} {
				remoteSecret, ok := currentSecretsByUID[s.remote.Name]
				if !ok {
					continue
				}

				if err := applySecret(poller, s.local, remoteSecret); err != nil {
					printer.Errorf("%v failed: %v\n", s.local, err)
				}
				delete(existingSecretsByUID[s.local.Name], s.remote.Name)
			}
		}
	}

	// existingSecretsByUID any leftover currentSecretsByUID
	for uid, secrets := range existingSecretsByUID {
		for _, secret := range secrets {
			cluster := mesh.GetCluster(uid)
			fmt.Printf("Pruning %v from %v\n", secret.Name, cluster)
			if err := deleteSecret(cluster, secret); err != nil {
				err := fmt.Errorf("failed to prune secret %v from cluster %v: %v", secret.Name, cluster, err)
				printer.Errorf(err.Error())
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

			poller := NewPoller()
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
			return Apply(mesh, poller, printer)
		},
	}
	opt.addFlags(c.PersistentFlags())
	return c
}
