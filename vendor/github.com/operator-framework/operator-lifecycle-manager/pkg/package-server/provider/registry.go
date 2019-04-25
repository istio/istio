package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/operator-framework/operator-registry/pkg/api"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	operatorsv1alpha1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/informers/externalversions"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/lib/queueinformer"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/metrics"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/apis/packagemanifest/v1alpha1"
)

const (
	defaultConnectionTimeout = 5 * time.Second
)

type sourceKey struct {
	name      string
	namespace string
}

type registryClient struct {
	api.RegistryClient
	source *operatorsv1alpha1.CatalogSource
	conn   *grpc.ClientConn
}

func newRegistryClient(source *operatorsv1alpha1.CatalogSource, conn *grpc.ClientConn) registryClient {
	return registryClient{
		RegistryClient: api.NewRegistryClient(conn),
		source:         source,
		conn:           conn,
	}
}

// RegistryProvider aggregates several `CatalogSources` and establishes gRPC connections to their registry servers.
type RegistryProvider struct {
	*queueinformer.Operator

	mu              sync.RWMutex
	globalNamespace string
	clients         map[sourceKey]registryClient
}

var _ PackageManifestProvider = &RegistryProvider{}

func NewRegistryProvider(crClient versioned.Interface, operator *queueinformer.Operator, wakeupInterval time.Duration, watchedNamespaces []string, globalNamespace string) *RegistryProvider {
	p := &RegistryProvider{
		Operator: operator,

		globalNamespace: globalNamespace,
		clients:         make(map[sourceKey]registryClient),
	}

	sourceHandlers := &cache.ResourceEventHandlerFuncs{
		DeleteFunc: p.catalogSourceDeleted,
	}
	for _, namespace := range watchedNamespaces {
		factory := externalversions.NewSharedInformerFactoryWithOptions(crClient, wakeupInterval, externalversions.WithNamespace(namespace))
		sourceInformer := factory.Operators().V1alpha1().CatalogSources()

		// Register queue and QueueInformer
		logrus.WithField("namespace", namespace).Info("watching catalogsources")
		queueName := fmt.Sprintf("%s/catalogsources", namespace)
		sourceQueue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), queueName)
		sourceQueueInformer := queueinformer.NewInformer(sourceQueue, sourceInformer.Informer(), p.syncCatalogSource, sourceHandlers, queueName, metrics.NewMetricsNil(), logrus.New())
		p.RegisterQueueInformer(sourceQueueInformer)
	}

	return p
}

func (p *RegistryProvider) getClient(key sourceKey) (registryClient, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	client, ok := p.clients[key]
	return client, ok
}

func (p *RegistryProvider) setClient(client registryClient, key sourceKey) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.clients[key] = client
}

func (p *RegistryProvider) removeClient(key sourceKey) (registryClient, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	client, ok := p.clients[key]
	if !ok {
		return registryClient{}, false
	}

	delete(p.clients, key)
	return client, true
}

func (p *RegistryProvider) syncCatalogSource(obj interface{}) (syncError error) {
	source, ok := obj.(*operatorsv1alpha1.CatalogSource)
	if !ok {
		logrus.Errorf("catalogsource type assertion failed: wrong type: %#v", obj)
	}

	logger := logrus.WithFields(logrus.Fields{
		"action":    "sync catalogsource",
		"name":      source.GetName(),
		"namespace": source.GetNamespace(),
	})

	if source.Status.RegistryServiceStatus == nil {
		logger.Debug("registry service is not ready for grpc connection")
		return
	}

	key := sourceKey{source.GetName(), source.GetNamespace()}
	client, ok := p.getClient(key)
	if ok {
		logger.Info("update detected, attempting to reset grpc connection")
		client.conn.ResetConnectBackoff()

		ctx, cancel := context.WithTimeout(context.TODO(), defaultConnectionTimeout)
		defer cancel()

		changed := client.conn.WaitForStateChange(ctx, connectivity.TransientFailure)
		if !changed {
			logger.Debugf("grpc connection reset timeout")
			syncError = fmt.Errorf("grpc connection reset timeout")
			return
		}

		logger.Info("grpc connection reset")
		return
	}

	logger.Info("attempting to add a new grpc connection")
	conn, err := grpc.Dial(source.Status.RegistryServiceStatus.Address(), grpc.WithInsecure())
	if err != nil {
		logger.WithField("err", err.Error()).Errorf("could not connect to registry service")
		syncError = err
		return
	}

	p.setClient(newRegistryClient(source, conn), key)
	logger.Info("new grpc connection added")

	return
}

func (p *RegistryProvider) catalogSourceDeleted(obj interface{}) {
	catsrc, ok := obj.(metav1.Object)
	if !ok {
		if !ok {
			tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
			if !ok {
				utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
				return
			}

			catsrc, ok = tombstone.Obj.(metav1.Object)
			if !ok {
				utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Namespace %#v", obj))
				return
			}
		}
	}

	logger := logrus.WithFields(logrus.Fields{
		"action":    "CatalogSource Deleted",
		"name":      catsrc.GetName(),
		"namespace": catsrc.GetNamespace(),
	})
	logger.Debugf("attempting to remove grpc connection")

	key := sourceKey{catsrc.GetName(), catsrc.GetNamespace()}
	client, removed := p.removeClient(key)
	if removed {
		err := client.conn.Close()
		if err != nil {
			logger.WithField("err", err.Error()).Error("error closing connection")
			utilruntime.HandleError(fmt.Errorf("error closing connection %s", err.Error()))
			return
		}
		logger.Debug("grpc connection removed")
		return
	}

	logger.Debugf("no gRPC connection to remove")

}

func (p *RegistryProvider) Get(namespace, name string) (*v1alpha1.PackageManifest, error) {
	logger := logrus.WithFields(logrus.Fields{
		"action":    "Get PackageManifest",
		"name":      name,
		"namespace": namespace,
	})

	pkgs, err := p.List(namespace)
	if err != nil {
		return nil, fmt.Errorf("could not list packages in namespace %s", namespace)
	}

	for _, pkg := range pkgs.Items {
		if pkg.GetName() == name {
			return &pkg, nil
		}
	}

	logger.Info("package not found")
	return nil, nil
}

func (p *RegistryProvider) List(namespace string) (*v1alpha1.PackageManifestList, error) {
	logger := logrus.WithFields(logrus.Fields{
		"action":    "List PackageManifests",
		"namespace": namespace,
	})

	p.mu.RLock()
	defer p.mu.RUnlock()

	pkgs := []v1alpha1.PackageManifest{}
	for _, client := range p.clients {
		if client.source.GetNamespace() == namespace || client.source.GetNamespace() == p.globalNamespace || namespace == metav1.NamespaceAll {
			logger.Debugf("found CatalogSource %s", client.source.GetName())

			stream, err := client.ListPackages(context.Background(), &api.ListPackageRequest{})
			if err != nil {
				logger.WithField("err", err.Error()).Warnf("error getting stream")
				continue
			}
			for {
				pkgName, err := stream.Recv()
				if err == io.EOF {
					break
				}

				if err != nil {
					logger.WithField("err", err.Error()).Warnf("error getting data")
					break
				}
				pkg, err := client.GetPackage(context.Background(), &api.GetPackageRequest{Name: pkgName.GetName()})
				if err != nil {
					logger.WithField("err", err.Error()).Warnf("error getting package")
					break
				}
				newPkg, err := toPackageManifest(pkg, client)
				if err != nil {
					logger.WithField("err", err.Error()).Warnf("error converting to packagemanifest")
					break
				}

				// Set request namespace to stop kube clients from complaining about global namespace mismatch.
				newPkg.SetNamespace(namespace)
				pkgs = append(pkgs, *newPkg)
			}
		}
	}

	return &v1alpha1.PackageManifestList{Items: pkgs}, nil
}

func toPackageManifest(pkg *api.Package, client registryClient) (*v1alpha1.PackageManifest, error) {
	pkgChannels := pkg.GetChannels()
	catsrc := client.source
	manifest := &v1alpha1.PackageManifest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              pkg.GetName(),
			Namespace:         catsrc.GetNamespace(),
			Labels:            catsrc.GetLabels(),
			CreationTimestamp: catsrc.GetCreationTimestamp(),
		},
		Status: v1alpha1.PackageManifestStatus{
			CatalogSource:            catsrc.GetName(),
			CatalogSourceDisplayName: catsrc.Spec.DisplayName,
			CatalogSourcePublisher:   catsrc.Spec.Publisher,
			CatalogSourceNamespace:   catsrc.GetNamespace(),
			PackageName:              pkg.Name,
			Channels:                 make([]v1alpha1.PackageChannel, len(pkgChannels)),
			DefaultChannel:           pkg.GetDefaultChannelName(),
		},
	}
	if manifest.GetLabels() == nil {
		manifest.SetLabels(labels.Set{})
	}
	manifest.ObjectMeta.Labels["catalog"] = manifest.Status.CatalogSource
	manifest.ObjectMeta.Labels["catalog-namespace"] = manifest.Status.CatalogSourceNamespace

	for i, pkgChannel := range pkgChannels {
		bundle, err := client.GetBundleForChannel(context.Background(), &api.GetBundleInChannelRequest{PkgName: pkg.GetName(), ChannelName: pkgChannel.GetName()})
		if err != nil {
			return nil, err
		}

		csv := operatorsv1alpha1.ClusterServiceVersion{}
		err = json.Unmarshal([]byte(bundle.GetCsvJson()), &csv)
		if err != nil {
			return nil, err
		}
		manifest.Status.Channels[i] = v1alpha1.PackageChannel{
			Name:           pkgChannel.GetName(),
			CurrentCSV:     csv.GetName(),
			CurrentCSVDesc: v1alpha1.CreateCSVDescription(&csv),
		}

		if manifest.Status.DefaultChannel != "" && csv.GetName() == manifest.Status.DefaultChannel || i == 0 {
			manifest.Status.Provider = v1alpha1.AppLink{
				Name: csv.Spec.Provider.Name,
				URL:  csv.Spec.Provider.URL,
			}
			manifest.ObjectMeta.Labels["provider"] = manifest.Status.Provider.Name
			manifest.ObjectMeta.Labels["provider-url"] = manifest.Status.Provider.URL
		}
	}

	return manifest, nil
}
