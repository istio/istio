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
	"bytes"
	"crypto/sha256"
	"fmt"

	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/multicluster"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/filesecrets"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

const (
	MultiClusterSecretLabel = "istio/multiCluster"
)

var (
	clusterType = monitoring.CreateLabel("ambient_cluster_type")

	clustersCount = monitoring.NewGauge(
		"ambient_istiod_managed_clusters",
		"Number of clusters managed by istiod",
	)

	localClusters  = clustersCount.With(clusterType.Value("local"))
	remoteClusters = clustersCount.With(clusterType.Value("remote"))
)

type ACTION int

const (
	Add ACTION = iota
	Update
)

// checked
func (a ACTION) String() string {
	switch a {
	case Add:
		return "Add"
	case Update:
		return "Update"
	}
	return "Unknown"
}

// checked
func (a *index) createRemoteCluster(secretKey types.NamespacedName, kubeConfig []byte, clusterID string) (*multicluster.Cluster, error) {
	client, err := a.clientBuilder(kubeConfig, cluster.ID(clusterID), a.remoteClientConfigOverrides...)
	if err != nil {
		return nil, err
	}
	return multicluster.NewCluster(
		cluster.ID(clusterID),
		client,
		&secretKey,
		ptr.Of(sha256.Sum256(kubeConfig)),
		nil,
	), nil
}

// checked
func (a *index) addSecret(name types.NamespacedName, s *corev1.Secret, debugger *krt.DebugHandler) error {
	return a.addRemoteConfig(name, s.Data, debugger)
}

// checked
func (a *index) addRemoteConfig(name types.NamespacedName, data map[string][]byte, debugger *krt.DebugHandler) error {
	secretKey := name.String()
	// First delete clusters
	existingClusters := a.cs.GetExistingClustersFor(secretKey)
	for _, existingCluster := range existingClusters {
		if _, ok := data[string(existingCluster.ID)]; !ok {
			a.deleteCluster(secretKey, existingCluster)
		}
	}

	var errs *multierror.Error
	addedClusters := make([]*multicluster.Cluster, 0, len(data))
	for clusterID, kubeConfig := range data {
		logger := log.WithLabels("cluster", clusterID, "secret", secretKey)
		if cluster.ID(clusterID) == a.ClusterID {
			logger.Infof("ignoring cluster as it would overwrite the config cluster")
			continue
		}

		action := Add
		if prev := a.cs.Get(secretKey, cluster.ID(clusterID)); prev != nil {
			action = Update
			// clusterID must be unique even across multiple secrets
			kubeConfigSha := sha256.Sum256(kubeConfig)
			if bytes.Equal(kubeConfigSha[:], prev.KubeConfigSha[:]) {
				logger.Infof("skipping update (kubeconfig are identical)")
				continue
			}
			// stop previous remote cluster
			prev.Stop()
			log.Debugf("Shutdown previous remote cluster %s for secret %s due to update", clusterID, secretKey)
		} else if a.cs.Contains(cluster.ID(clusterID)) {
			// if the cluster has been registered before by another secret, ignore the new one.
			logger.Warnf("cluster has already been registered")
			continue
		}
		logger.Infof("%s cluster", action)

		remoteCluster, err := a.createRemoteCluster(name, kubeConfig, clusterID)
		if err != nil {
			logger.Errorf("%s cluster: create remote cluster failed: %v", action, err)
			errs = multierror.Append(errs, err)
			continue
		}

		// Store before we run so that a bad secret doesn't block adding the cluster to the store.
		// This is necessary so that changes to the bad secret are processed as an update and the bad
		// cluster is stopped and shutdown.
		a.cs.Store(secretKey, remoteCluster.ID, remoteCluster)
		// Run returns after initializing the cluster's fields; it runs all of the expensive operations
		// in a goroutine (including discovery filter sync), so we can safely call it synchronously here.
		remoteCluster.Run(a.meshConfig, debugger)
		addedClusters = append(addedClusters, remoteCluster)
	}

	syncers := slices.Map(addedClusters, func(c *multicluster.Cluster) cache.InformerSynced { return c.HasSynced })
	// Don't allow the event handler to continue without the cluster being synced
	if !kube.WaitForCacheSync("remoteClusters", a.stop, syncers...) {
		return fmt.Errorf("timed out waiting for remote clusters %#v to sync", addedClusters)
	}

	log.Info("remotesecret.goL155 addRemoteConfig completed")                  // hit
	log.Infof("remotesecret.goL156 Number of remote clusters: %d", a.cs.Len()) // hit
	return errs.ErrorOrNil()
}

// checked
func (a *index) deleteSecret(secretKey string) {
	for _, cluster := range a.cs.GetExistingClustersFor(secretKey) {
		if cluster.ID == a.ClusterID {
			log.Infof("ignoring delete cluster %v from secret %v as it would overwrite the config cluster", a.ClusterID, secretKey)
			continue
		}

		a.deleteCluster(secretKey, cluster)
	}

	log.Infof("remotesecret.goL171 Number of remote clusters: %d", a.cs.Len())
}

// checked
func (a *index) deleteCluster(secretKey string, cluster *multicluster.Cluster) {
	log.Infof("Deleting cluster_id=%v configured by secret=%v", cluster.ID, secretKey)
	cluster.Stop()
	// The delete event will be processed within the ClusterStore
	a.cs.Delete(secretKey, cluster.ID)
	cluster.Client.Shutdown() // Shutdown all of the informers so that the goroutines won't leak
}

// checked
func (a *index) processSecretEvent(key types.NamespacedName) error {
	log.Infof("processing secret event for secret %s", key)
	scrt := ptr.Flatten(a.secrets.GetKey(key.String()))
	if scrt != nil {
		log.Debugf("secret %s exists in secret collection, processing it", key)
		if err := a.addSecret(key, scrt, a.Debugger); err != nil {
			return fmt.Errorf("error adding secret %s: %v", key, err)
		}
	} else {
		log.Debugf("secret %s does not exist in secret collection, deleting it", key)
		a.deleteSecret(key.String())
	}
	remoteClusters.Record(float64(a.cs.Len()))
	return nil
}

func (a *index) processKubeconfigEvent(key types.NamespacedName) error {
	log.Infof("processing kubeconfig event for %s", key)
	entry := a.kubeconfigs.GetKey(key.String())
	if entry != nil {
		log.Debugf("kubeconfig %s exists in collection, processing it", key)
		data := map[string][]byte{entry.ClusterID: entry.Kubeconfig}
		if err := a.addRemoteConfig(key, data, a.Debugger); err != nil {
			return fmt.Errorf("error adding kubeconfig %s: %v", key, err)
		}
	} else {
		log.Debugf("kubeconfig %s does not exist in collection, deleting it", key)
		a.deleteSecret(key.String())
	}
	remoteClusters.Record(float64(a.cs.Len()))
	return nil
}

func (a *index) buildRemoteClustersCollection(
	options Options,
	opts krt.OptionsBuilder,
	configOverrides ...func(*rest.Config),
) krt.Collection[*multicluster.Cluster] {
	// init gauges
	localClusters.Record(1.0)
	remoteClusters.Record(0.0)

	if features.RemoteClusterSecretPath != "" {
		kubeconfigs, err := filesecrets.NewKubeconfigCollection(
			features.RemoteClusterSecretPath,
			options.SystemNamespace,
			opts.WithName("RemoteKubeconfigs")...,
		)
		if err != nil {
			log.Errorf("Failed to load remote kubeconfigs from %q: %v", features.RemoteClusterSecretPath, err)
			kubeconfigs = krt.NewStaticCollection[filesecrets.KubeconfigEntry](nil, nil, opts.WithName("RemoteKubeconfigs")...)
		}
		a.kubeconfigs = kubeconfigs

		// N.B Informer collections don't call handler before marking synced, so
		// RemoteClusters will be synced pretty immediately.
		kubeconfigs.Register(func(o krt.Event[filesecrets.KubeconfigEntry]) {
			item := o.Latest()
			key := types.NamespacedName{Name: item.Name, Namespace: item.Namespace}
			err := a.processKubeconfigEvent(key)
			if err != nil {
				log.Errorf("error processing kubeconfig %s: %v", krt.GetKey(item), err)
			}
		})

		go func() {
			if !kubeconfigs.WaitUntilSynced(a.stop) {
				log.Errorf("Timed out waiting for remote kubeconfigs to sync")
			}
			a.cs.MarkSynced() // Mark clusters as synced iff kubeconfigs are synced
		}()
	} else {
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

		secrets := kclient.NewFiltered[*corev1.Secret](informerClient, kclient.Filter{
			Namespace:     options.SystemNamespace,
			LabelSelector: MultiClusterSecretLabel + "=true",
		})
		Secrets := krt.WrapClient(secrets, opts.WithName("RemoteSecrets")...)
		a.secrets = Secrets

		// N.B Informer collections don't call handler before marking synced, so
		// RemoteClusters will be synced pretty immediately.
		Secrets.Register(func(o krt.Event[*corev1.Secret]) {
			s := o.Latest()
			err := a.processSecretEvent(config.NamespacedName(s))
			if err != nil {
				log.Errorf("error processing secret %s: %v", krt.GetKey(s), err)
			}
		})

		go func() {
			if !Secrets.WaitUntilSynced(a.stop) {
				log.Errorf("Timed out waiting for remote secrets to sync")
			}
			a.cs.MarkSynced() // Mark clusters as synced iff secrets are synced
		}()
	}

	Clusters := krt.NewManyFromNothing(func(ctx krt.HandlerContext) []*multicluster.Cluster {
		a.cs.MarkDependant(ctx) // Subscribe to updates from the clusterStore
		remoteClustersBySecretThenID := a.cs.AllReady()
		var remoteClusters []*multicluster.Cluster
		for _, clusters := range remoteClustersBySecretThenID {
			for _, cluster := range clusters {
				remoteClusters = append(remoteClusters, cluster)
			}
		}
		return remoteClusters
	}, opts.WithName("RemoteClusters")...)

	return Clusters
}
