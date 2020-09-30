package kube

import (
	"fmt"
	"sync"

	"istio.io/istio/pilot/pkg/secrets"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/secretcontroller"
	"istio.io/pkg/log"
)

// Multicluster structure holds the remote kube Controllers and multicluster specific attributes.
type Multicluster struct {
	remoteKubeControllers map[string]*SecretsController
	m                     sync.Mutex // protects remoteKubeControllers
	secretController      *secretcontroller.Controller
	localCluster          string
}

var _ secrets.Controller = &Multicluster{}

func NewMulticluster(client kube.Client, localClusterId, secretNamespace string) *Multicluster {
	m := &Multicluster{
		remoteKubeControllers: map[string]*SecretsController{},
		localCluster:          localClusterId,
	}
	// Add the local cluster
	if err := m.AddMemberCluster(client, localClusterId); err != nil {
		return nil
	}
	sc := secretcontroller.StartSecretController(client,
		m.AddMemberCluster,
		m.UpdateMemberCluster,
		m.DeleteMemberCluster,
		secretNamespace)
	m.secretController = sc
	return m
}

func (m *Multicluster) AddMemberCluster(clients kube.Client, key string) error {
	stopCh := make(chan struct{})
	log.Infof("initializing Kubernetes credential reader for cluster %v", key)
	sc := NewSecretsController(clients, key)
	m.m.Lock()
	m.remoteKubeControllers[key] = sc
	m.m.Unlock()
	clients.RunAndWait(stopCh)
	return nil
}

func (m *Multicluster) UpdateMemberCluster(clients kube.Client, key string) error {
	if err := m.DeleteMemberCluster(key); err != nil {
		return err
	}
	return m.AddMemberCluster(clients, key)
}

func (m *Multicluster) DeleteMemberCluster(key string) error {
	m.m.Lock()
	delete(m.remoteKubeControllers, key)
	m.m.Unlock()
	return nil
}

func (m *Multicluster) GetKeyAndCert(name, namespace string) (key []byte, cert []byte) {
	// Prefer local cluster
	if lk, lc := m.remoteKubeControllers[m.localCluster].GetKeyAndCert(name, namespace); lk != nil && lc != nil {
		return lk, lc
	}
	// Search through all clusters
	for _, c := range m.remoteKubeControllers {
		k, c := c.GetKeyAndCert(name, namespace)
		if k != nil && c != nil {
			return k, c
		}
	}
	return nil, nil
}

func (m *Multicluster) GetCaCert(name, namespace string) (cert []byte) {
	// Prefer local cluster
	if lk := m.remoteKubeControllers[m.localCluster].GetCaCert(name, namespace); lk != nil {
		return lk
	}
	// Search through all clusters
	for _, c := range m.remoteKubeControllers {
		k := c.GetCaCert(name, namespace)
		if k != nil {
			return k
		}
	}
	return nil
}

func (m *Multicluster) Authorize(serviceAccount, namespace, clusterID string) error {
	if c, f := m.remoteKubeControllers[clusterID]; f {
		// For auth, we always lookup in the same namespace as the proxy is running
		return c.Authorize(serviceAccount, namespace, clusterID)
	} else {
		return fmt.Errorf("client for cluster %q not found", clusterID)
	}
}

func (m *Multicluster) AddEventHandler(f func(name string, namespace string)) {
	for _, c := range m.remoteKubeControllers {
		c.AddEventHandler(f)
	}
}
