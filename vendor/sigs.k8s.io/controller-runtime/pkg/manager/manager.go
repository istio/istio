/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package manager

import (
	"fmt"
	"net"
	"time"

	"github.com/go-logr/logr"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	internalrecorder "sigs.k8s.io/controller-runtime/pkg/internal/recorder"
	"sigs.k8s.io/controller-runtime/pkg/leaderelection"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/recorder"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
)

// Manager initializes shared dependencies such as Caches and Clients, and provides them to Runnables.
// A Manager is required to create Controllers.
type Manager interface {
	// Add will set reqeusted dependencies on the component, and cause the component to be
	// started when Start is called.  Add will inject any dependencies for which the argument
	// implements the inject interface - e.g. inject.Client
	Add(Runnable) error

	// SetFields will set any dependencies on an object for which the object has implemented the inject
	// interface - e.g. inject.Client.
	SetFields(interface{}) error

	// Start starts all registered Controllers and blocks until the Stop channel is closed.
	// Returns an error if there is an error starting any controller.
	Start(<-chan struct{}) error

	// GetConfig returns an initialized Config
	GetConfig() *rest.Config

	// GetScheme returns and initialized Scheme
	GetScheme() *runtime.Scheme

	// GetAdmissionDecoder returns the runtime.Decoder based on the scheme.
	GetAdmissionDecoder() types.Decoder

	// GetClient returns a client configured with the Config
	GetClient() client.Client

	// GetFieldIndexer returns a client.FieldIndexer configured with the client
	GetFieldIndexer() client.FieldIndexer

	// GetCache returns a cache.Cache
	GetCache() cache.Cache

	// GetRecorder returns a new EventRecorder for the provided name
	GetRecorder(name string) record.EventRecorder

	// GetRESTMapper returns a RESTMapper
	GetRESTMapper() meta.RESTMapper
}

// Options are the arguments for creating a new Manager
type Options struct {
	// Scheme is the scheme used to resolve runtime.Objects to GroupVersionKinds / Resources
	// Defaults to the kubernetes/client-go scheme.Scheme
	Scheme *runtime.Scheme

	// MapperProvider provides the rest mapper used to map go types to Kubernetes APIs
	MapperProvider func(c *rest.Config) (meta.RESTMapper, error)

	// SyncPeriod determines the minimum frequency at which watched resources are
	// reconciled. A lower period will correct entropy more quickly, but reduce
	// responsiveness to change if there are many watched resources. Change this
	// value only if you know what you are doing. Defaults to 10 hours if unset.
	SyncPeriod *time.Duration

	// LeaderElection determines whether or not to use leader election when
	// starting the manager.
	LeaderElection bool

	// LeaderElectionNamespace determines the namespace in which the leader
	// election configmap will be created.
	LeaderElectionNamespace string

	// LeaderElectionID determines the name of the configmap that leader election
	// will use for holding the leader lock.
	LeaderElectionID string

	// Namespace if specified restricts the manager's cache to watch objects in the desired namespace
	// Defaults to all namespaces
	// Note: If a namespace is specified then controllers can still Watch for a cluster-scoped resource e.g Node
	// For namespaced resources the cache will only hold objects from the desired namespace.
	Namespace string

	// MetricsBindAddress is the TCP address that the controller should bind to
	// for serving prometheus metrics
	MetricsBindAddress string

	// Functions to all for a user to customize the values that will be injected.

	// NewCache is the function that will create the cache to be used
	// by the manager. If not set this will use the default new cache function.
	NewCache NewCacheFunc

	// NewClient will create the client to be used by the manager.
	// If not set this will create the default DelegatingClient that will
	// use the cache for reads and the client for writes.
	NewClient NewClientFunc

	// Dependency injection for testing
	newRecorderProvider func(config *rest.Config, scheme *runtime.Scheme, logger logr.Logger) (recorder.Provider, error)
	newResourceLock     func(config *rest.Config, recorderProvider recorder.Provider, options leaderelection.Options) (resourcelock.Interface, error)
	newAdmissionDecoder func(scheme *runtime.Scheme) (types.Decoder, error)
	newMetricsListener  func(addr string) (net.Listener, error)
}

// NewCacheFunc allows a user to define how to create a cache
type NewCacheFunc func(config *rest.Config, opts cache.Options) (cache.Cache, error)

// NewClientFunc allows a user to define how to create a client
type NewClientFunc func(cache cache.Cache, config *rest.Config, options client.Options) (client.Client, error)

// Runnable allows a component to be started.
type Runnable interface {
	// Start starts running the component.  The component will stop running when the channel is closed.
	// Start blocks until the channel is closed or an error occurs.
	Start(<-chan struct{}) error
}

// RunnableFunc implements Runnable
type RunnableFunc func(<-chan struct{}) error

// Start implements Runnable
func (r RunnableFunc) Start(s <-chan struct{}) error {
	return r(s)
}

// New returns a new Manager for creating Controllers.
func New(config *rest.Config, options Options) (Manager, error) {
	// Initialize a rest.config if none was specified
	if config == nil {
		return nil, fmt.Errorf("must specify Config")
	}

	// Set default values for options fields
	options = setOptionsDefaults(options)

	// Create the mapper provider
	mapper, err := options.MapperProvider(config)
	if err != nil {
		log.Error(err, "Failed to get API Group-Resources")
		return nil, err
	}

	// Create the cache for the cached read client and registering informers
	cache, err := options.NewCache(config, cache.Options{Scheme: options.Scheme, Mapper: mapper, Resync: options.SyncPeriod, Namespace: options.Namespace})
	if err != nil {
		return nil, err
	}

	writeObj, err := options.NewClient(cache, config, client.Options{Scheme: options.Scheme, Mapper: mapper})
	if err != nil {
		return nil, err
	}
	// Create the recorder provider to inject event recorders for the components.
	// TODO(directxman12): the log for the event provider should have a context (name, tags, etc) specific
	// to the particular controller that it's being injected into, rather than a generic one like is here.
	recorderProvider, err := options.newRecorderProvider(config, options.Scheme, log.WithName("events"))
	if err != nil {
		return nil, err
	}

	// Create the resource lock to enable leader election)
	resourceLock, err := options.newResourceLock(config, recorderProvider, leaderelection.Options{
		LeaderElection:          options.LeaderElection,
		LeaderElectionID:        options.LeaderElectionID,
		LeaderElectionNamespace: options.LeaderElectionNamespace,
	})
	if err != nil {
		return nil, err
	}

	admissionDecoder, err := options.newAdmissionDecoder(options.Scheme)
	if err != nil {
		return nil, err
	}

	// Create the mertics listener. This will throw an error if the metrics bind
	// address is invalid or already in use.
	metricsListener, err := options.newMetricsListener(options.MetricsBindAddress)
	if err != nil {
		return nil, err
	}

	stop := make(chan struct{})

	return &controllerManager{
		config:           config,
		scheme:           options.Scheme,
		admissionDecoder: admissionDecoder,
		errChan:          make(chan error),
		cache:            cache,
		fieldIndexes:     cache,
		client:           writeObj,
		recorderProvider: recorderProvider,
		resourceLock:     resourceLock,
		mapper:           mapper,
		metricsListener:  metricsListener,
		internalStop:     stop,
		internalStopper:  stop,
	}, nil
}

// defaultNewClient creates the default caching client
func defaultNewClient(cache cache.Cache, config *rest.Config, options client.Options) (client.Client, error) {
	// Create the Client for Write operations.
	c, err := client.New(config, options)
	if err != nil {
		return nil, err
	}

	return &client.DelegatingClient{
		Reader: &client.DelegatingReader{
			CacheReader:  cache,
			ClientReader: c,
		},
		Writer:       c,
		StatusClient: c,
	}, nil
}

// setOptionsDefaults set default values for Options fields
func setOptionsDefaults(options Options) Options {
	// Use the Kubernetes client-go scheme if none is specified
	if options.Scheme == nil {
		options.Scheme = scheme.Scheme
	}

	if options.MapperProvider == nil {
		options.MapperProvider = apiutil.NewDiscoveryRESTMapper
	}

	// Allow newClient to be mocked
	if options.NewClient == nil {
		options.NewClient = defaultNewClient
	}

	// Allow newCache to be mocked
	if options.NewCache == nil {
		options.NewCache = cache.New
	}

	// Allow newRecorderProvider to be mocked
	if options.newRecorderProvider == nil {
		options.newRecorderProvider = internalrecorder.NewProvider
	}

	// Allow newResourceLock to be mocked
	if options.newResourceLock == nil {
		options.newResourceLock = leaderelection.NewResourceLock
	}

	if options.newAdmissionDecoder == nil {
		options.newAdmissionDecoder = admission.NewDecoder
	}

	if options.newMetricsListener == nil {
		options.newMetricsListener = metrics.NewListener
	}

	return options
}
