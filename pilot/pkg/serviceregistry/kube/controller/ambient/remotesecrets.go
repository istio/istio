// Copyright Istio Authors
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

package ambient

import (
	"crypto/sha256"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/ptr"
)

const (
	MultiClusterSecretLabel = "istio/multiCluster"
)

var (
	clusterLabel = monitoring.CreateLabel("cluster")
	timeouts     = monitoring.NewSum(
		"remote_cluster_sync_timeouts_total",
		"Number of times remote clusters took too long to sync, causing slow startup that excludes remote clusters.",
	)

	clusterType = monitoring.CreateLabel("cluster_type")

	clustersCount = monitoring.NewGauge(
		"istiod_managed_clusters",
		"Number of clusters managed by istiod",
	)

	localClusters  = clustersCount.With(clusterType.Value("local"))
	remoteClusters = clustersCount.With(clusterType.Value("remote"))
)

type Cluster struct {
	// ID of the cluster.
	ID cluster.ID
	// Client for accessing the cluster.
	Client kube.Client

	// TODO: Figure out if we really need this and how to use it in krt
	kubeConfigSha  [sha256.Size]byte
	clusterDetails krt.Singleton[ClusterDetails]
	filter         kubetypes.DynamicObjectFilter
}

type ClusterDetails struct {
	Network network.ID
}

// ClientBuilder builds a new kube.Client from a kubeconfig. Mocked out for testing
type ClientBuilder = func(kubeConfig []byte, clusterId cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error)

func buildRemoteClustersCollection(
	options Options,
	opts krt.OptionsBuilder,
	builder ClientBuilder,
	filter kclient.Filter,
	configOverrides ...func(*rest.Config),
) krt.Collection[*Cluster] {
	informerClient := options.Client

	// When these two are set to true, Istiod will be watching the namespace in which
	// Istiod is running on the external cluster. Use the inCluster credentials to
	// create a kubeclientset
	if features.LocalClusterSecretWatcher && features.ExternalIstiod {
		config, err := kube.InClusterConfig(configOverrides...)
		if err != nil {
			log.Errorf("Could not get istiod incluster configuration: %v", err)
			return nil
		}
		log.Info("Successfully retrieved incluster config.")

		localKubeClient, err := kube.NewClient(kube.NewClientConfigForRestConfig(config), options.ClusterID)
		if err != nil {
			log.Errorf("Could not create a client to access local cluster API server: %v", err)
			return nil
		}
		log.Infof("Successfully created in cluster kubeclient at %s", localKubeClient.RESTConfig().Host)
		informerClient = localKubeClient
	}

	// init gauges
	localClusters.Record(1.0)
	remoteClusters.Record(0.0)

	secrets := kclient.NewFiltered[*corev1.Secret](informerClient, kclient.Filter{
		Namespace:     options.SystemNamespace,
		LabelSelector: MultiClusterSecretLabel + "=true",
	})
	Secrets := krt.WrapClient(secrets, opts.WithName("RemoteSecrets")...)

	namespaces := kclient.NewFiltered[*corev1.Namespace](informerClient, filter)
	Namespaces := krt.WrapClient(namespaces, opts.WithName("Namespaces")...)

	LocalCluster := krt.NewSingleton(func(ctx krt.HandlerContext) *Cluster {
		return &Cluster{
			ID:     options.ClusterID,
			Client: informerClient,
			clusterDetails: krt.NewSingleton(func(ctx krt.HandlerContext) *ClusterDetails {
				ns := ptr.Flatten(krt.FetchOne(ctx, Namespaces, krt.FilterKey(options.SystemNamespace)))
				if ns == nil {
					return nil
				}
				nw, f := ns.Labels[label.TopologyNetwork.Name]
				if !f {
					nw = ""
				}
				return &ClusterDetails{
					Network: network.ID(nw),
				}
			}),
			filter: filter.ObjectFilter, // TODO: is this correct?
		}
	})
	Clusters := krt.NewManyCollection(Secrets, func(ctx krt.HandlerContext, s *corev1.Secret) []*Cluster {
		secretKey := krt.GetKey(s)
		var clusters []*Cluster
		for clusterID, kubeConfig := range s.Data {
			logger := log.WithLabels("cluster", clusterID, "secret", secretKey)
			if cluster.ID(clusterID) == options.ClusterID {
				logger.Infof("ignoring cluster as it would overwrite the config cluster")
				continue
			}
			client, err := builder(kubeConfig, cluster.ID(clusterID), configOverrides...)
			if err != nil {
				log.Errorf("Failed to create client for cluster %s from secret %s: %v", clusterID, secretKey, err)
				continue
			}
			remoteNamespaces := kclient.NewFiltered[*corev1.Namespace](client, filter)
			RemoteNamespaces := krt.WrapClient(remoteNamespaces, opts.WithName("RemoteNamespaces")...)
			details := krt.NewSingleton(func(ctx krt.HandlerContext) *ClusterDetails {
				ns := ptr.Flatten(krt.FetchOne(ctx, RemoteNamespaces, krt.FilterKey(options.SystemNamespace)))
				if ns == nil {
					return nil
				}
				nw, f := ns.Labels[label.TopologyNetwork.Name]
				if !f {
					nw = ""
				}
				return &ClusterDetails{
					Network: network.ID(nw),
				}
			}, opts.WithName("RemoteClusters")...)
			cluster := &Cluster{
				ID:             cluster.ID(clusterID),
				Client:         client,
				kubeConfigSha:  sha256.Sum256(kubeConfig),
				clusterDetails: details,
			}
			clusters = append(clusters, cluster)
		}
		if len(clusters) == 0 {
			return nil
		}

		// Push the local cluster to the front of the list
		// TODO: is this ordering helpful?
		return append([]*Cluster{LocalCluster.Get()}, clusters...)
	})

	return Clusters
}

func (c *Cluster) ResourceName() string {
	return c.ID.String()
}
