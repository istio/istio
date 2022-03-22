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

package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/ryanuber/go-glob"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/annotation"
	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/file"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/scope"
	mesh_const "istio.io/istio/pkg/config/legacy/mesh"
	"istio.io/istio/pkg/config/legacy/util/kuberesource"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	kubelib "istio.io/istio/pkg/kube"
)

// IstiodAnalyzer handles local analysis of k8s event sources, both live and file-based
type IstiodAnalyzer struct {
	// internalStore stores synthetic configs for analysis (mesh config, etc)
	internalStore model.ConfigStore
	// stores contains all the (non file) config sources to analyze
	stores []model.ConfigStoreCache
	// fileSource contains all file bases sources
	fileSource *file.KubeSource

	analyzer       *analysis.CombinedAnalyzer
	namespace      resource.Namespace
	istioNamespace resource.Namespace

	initializedStore model.ConfigStoreCache

	// List of code and resource suppressions to exclude messages on
	suppressions []AnalysisSuppression

	// Mesh config for this analyzer. This can come from multiple sources, and the last added version will take precedence.
	meshCfg *v1alpha1.MeshConfig

	// Mesh networks config for this analyzer.
	meshNetworks *v1alpha1.MeshNetworks

	// Which kube resources are used by this analyzer
	// Derived from metadata and the specified analyzer and transformer providers
	kubeResources collection.Schemas

	// Hook function called when a collection is used in analysis
	collectionReporter CollectionReporterFn

	clientsToRun []kubelib.Client
}

// NewSourceAnalyzer is a drop-in replacement for the galley function, adapting to istiod analyzer.
func NewSourceAnalyzer(analyzer *analysis.CombinedAnalyzer, namespace, istioNamespace resource.Namespace,
	cr CollectionReporterFn, serviceDiscovery bool, _ time.Duration) *IstiodAnalyzer {
	return NewIstiodAnalyzer(analyzer, namespace, istioNamespace, cr, serviceDiscovery)
}

// NewIstiodAnalyzer creates a new IstiodAnalyzer with no sources. Use the Add*Source
// methods to add sources in ascending precedence order,
// then execute Analyze to perform the analysis
func NewIstiodAnalyzer(analyzer *analysis.CombinedAnalyzer, namespace,
	istioNamespace resource.Namespace, cr CollectionReporterFn, serviceDiscovery bool) *IstiodAnalyzer {
	// collectionReporter hook function defaults to no-op
	if cr == nil {
		cr = func(collection.Name) {}
	}

	// Get the closure of all input collections for our analyzer, paying attention to transforms
	kubeResources := kuberesource.SkipExcludedCollections(
		analyzer.Metadata().Inputs,
		kuberesource.DefaultExcludedResourceKinds(),
		serviceDiscovery)

	mcfg := mesh.DefaultMeshConfig()
	sa := &IstiodAnalyzer{
		meshCfg:            mcfg,
		meshNetworks:       mesh.DefaultMeshNetworks(),
		analyzer:           analyzer,
		namespace:          namespace,
		internalStore:      memory.Make(collection.SchemasFor(collections.IstioMeshV1Alpha1MeshNetworks, collections.IstioMeshV1Alpha1MeshConfig)),
		istioNamespace:     istioNamespace,
		kubeResources:      kubeResources,
		collectionReporter: cr,
	}

	return sa
}

// ReAnalyze loads the sources and executes the analysis, assuming init is already called
func (sa *IstiodAnalyzer) ReAnalyze(cancel <-chan struct{}) (AnalysisResult, error) {
	var result AnalysisResult
	store := sa.initializedStore
	result.ExecutedAnalyzers = sa.analyzer.AnalyzerNames()
	result.SkippedAnalyzers = sa.analyzer.RemoveSkipped(store.Schemas())

	cache.WaitForCacheSync(cancel,
		store.HasSynced)

	ctx := NewContext(store, cancel, sa.collectionReporter)

	sa.analyzer.Analyze(ctx)

	namespaces := make(map[resource.Namespace]struct{})
	if sa.namespace != "" {
		namespaces[sa.namespace] = struct{}{}
	}
	// TODO: analysis is run for all namespaces, even if they are requested to be filtered.
	msgs := filterMessages(ctx.(*istiodContext).messages, namespaces, sa.suppressions)
	result.Messages = msgs.SortedDedupedCopy()

	return result, nil
}

// Analyze loads the sources and executes the analysis
func (sa *IstiodAnalyzer) Analyze(cancel <-chan struct{}) (AnalysisResult, error) {
	err2 := sa.Init(cancel)
	if err2 != nil {
		return AnalysisResult{}, err2
	}
	return sa.ReAnalyze(cancel)
}

func (sa *IstiodAnalyzer) Init(cancel <-chan struct{}) error {
	// We need at least one non-meshcfg source
	if len(sa.stores) == 0 && sa.fileSource == nil {
		return fmt.Errorf("at least one file and/or Kubernetes source must be provided")
	}

	// TODO: there's gotta be a better way to convert v1meshconfig to config.Config...
	// Create a store containing mesh config. There should be exactly one.
	_, err := sa.internalStore.Create(config.Config{
		Meta: config.Meta{
			Name:             mesh_const.MeshConfigResourceName.Name.String(),
			Namespace:        mesh_const.MeshConfigResourceName.Namespace.String(),
			GroupVersionKind: collections.IstioMeshV1Alpha1MeshConfig.Resource().GroupVersionKind(),
		},
		Spec: sa.meshCfg,
	})
	if err != nil {
		return fmt.Errorf("something unexpected happened while creating the meshconfig: %s", err)
	}
	// Create a store containing meshnetworks. There should be exactly one.
	_, err = sa.internalStore.Create(config.Config{
		Meta: config.Meta{
			Name:             mesh_const.MeshNetworksResourceName.Name.String(),
			Namespace:        mesh_const.MeshNetworksResourceName.Namespace.String(),
			GroupVersionKind: collections.IstioMeshV1Alpha1MeshNetworks.Resource().GroupVersionKind(),
		},
		Spec: sa.meshNetworks,
	})
	if err != nil {
		return fmt.Errorf("something unexpected happened while creating the meshnetworks: %s", err)
	}
	allstores := append(sa.stores, dfCache{ConfigStore: sa.internalStore})
	if sa.fileSource != nil {
		allstores = append(allstores, sa.fileSource)
	}

	for _, c := range sa.clientsToRun {
		// TODO: this could be parallel
		c.RunAndWait(cancel)
	}

	store, err := aggregate.MakeWriteableCache(allstores, nil)
	if err != nil {
		return err
	}
	go store.Run(cancel)
	sa.initializedStore = store
	return nil
}

type dfCache struct {
	model.ConfigStore
}

func (d dfCache) RegisterEventHandler(kind config.GroupVersionKind, handler model.EventHandler) {
	panic("implement me")
}

// Run intentionally left empty
func (d dfCache) Run(_ <-chan struct{}) {
}

func (d dfCache) SetWatchErrorHandler(f func(r *cache.Reflector, err error)) error {
	panic("implement me")
}

func (d dfCache) HasSynced() bool {
	return true
}

// SetSuppressions will set the list of suppressions for the analyzer. Any
// resource that matches the provided suppression will not be included in the
// final message output.
func (sa *IstiodAnalyzer) SetSuppressions(suppressions []AnalysisSuppression) {
	sa.suppressions = suppressions
}

// AddReaderKubeSource adds a source based on the specified k8s yaml files to the current IstiodAnalyzer
func (sa *IstiodAnalyzer) AddReaderKubeSource(readers []ReaderSource) error {
	var src *file.KubeSource
	if sa.fileSource != nil {
		src = sa.fileSource
	} else {
		src = file.NewKubeSource(sa.kubeResources)
		sa.fileSource = src
	}
	src.SetDefaultNamespace(sa.namespace)

	var errs error

	// If we encounter any errors reading or applying files, track them but attempt to continue
	for _, r := range readers {
		by, err := io.ReadAll(r.Reader)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}

		if err = src.ApplyContent(r.Name, string(by)); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	return errs
}

// AddRunningKubeSource adds a source based on a running k8s cluster to the current IstiodAnalyzer
// Also tries to get mesh config from the running cluster, if it can
func (sa *IstiodAnalyzer) AddRunningKubeSource(c kubelib.Client) {
	// TODO: are either of these string constants intended to vary?
	// This gets us only istio/ ones
	store, err := crdclient.NewForSchemas(context.Background(), c, "default",
		"cluster.local", sa.kubeResources)
	// RunAndWait must be called after NewForSchema so that the informers are all created and started.
	if err != nil {
		scope.Analysis.Errorf("error adding kube crdclient: %v", err)
		return
	}
	sa.stores = append(sa.stores, store)
	err = store.SetWatchErrorHandler(func(r *cache.Reflector, err error) {
		// failed resources will never be synced, which causes the process to hang indefinitely.
		// better to fail fast, and get a good idea for the failure.
		scope.Analysis.Errorf("Failed to watch crd resource for analysis: %s", err)
	})
	if err != nil {
		scope.Analysis.Errorf("error setting up error handling for kube crdclient: %v", err)
		return
	}
	sa.clientsToRun = append(sa.clientsToRun, c)

	// Since we're using a running k8s source, try to get meshconfig and meshnetworks from the configmap.
	if err := sa.addRunningKubeIstioConfigMapSource(c); err != nil {
		_, err := c.CoreV1().Namespaces().Get(context.TODO(), sa.istioNamespace.String(), metav1.GetOptions{})
		if kerrors.IsNotFound(err) {
			// An AnalysisMessage already show up to warn the absence of istio-system namespace, so making it debug level.
			scope.Analysis.Debugf("%v namespace not found. Istio may not be installed in the target cluster. "+
				"Using default mesh configuration values for analysis", sa.istioNamespace.String())
		} else if err != nil {
			scope.Analysis.Errorf("error getting mesh config from running kube source: %v", err)
		}
	}
}

// AddSource adds a source based on user supplied configstore to the current IstiodAnalyzer
// Assumes that the source has same or subset of resource types that this analyzer is configured with.
// This can be used by external users who import the analyzer as a module within their own controllers.
func (sa *IstiodAnalyzer) AddSource(src model.ConfigStoreCache) {
	sa.stores = append(sa.stores, src)
}

// AddFileKubeMeshConfig gets mesh config from the specified yaml file
func (sa *IstiodAnalyzer) AddFileKubeMeshConfig(file string) error {
	by, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	cfg, err := mesh.ApplyMeshConfigDefaults(string(by))
	if err != nil {
		return err
	}

	sa.meshCfg = cfg
	return nil
}

// AddFileKubeMeshNetworks gets a file meshnetworks and add it to the analyzer.
func (sa *IstiodAnalyzer) AddFileKubeMeshNetworks(file string) error {
	mn, err := mesh.ReadMeshNetworks(file)
	if err != nil {
		return err
	}

	sa.meshNetworks = mn
	return nil
}

// AddDefaultResources adds some basic dummy Istio resources, based on mesh configuration.
// This is useful for files-only analysis cases where we don't expect the user to be including istio system resources
// and don't want to generate false positives because they aren't there.
// Respect mesh config when deciding which default resources should be generated
func (sa *IstiodAnalyzer) AddDefaultResources() error {
	var readers []ReaderSource

	if sa.meshCfg.GetIngressControllerMode() != v1alpha1.MeshConfig_OFF {
		ingressResources, err := getDefaultIstioIngressGateway(sa.istioNamespace.String(), sa.meshCfg.GetIngressService())
		if err != nil {
			return err
		}
		readers = append(readers, ReaderSource{Reader: strings.NewReader(ingressResources), Name: "internal-ingress"})
	}

	if len(readers) == 0 {
		return nil
	}

	return sa.AddReaderKubeSource(readers)
}

func (sa *IstiodAnalyzer) addRunningKubeIstioConfigMapSource(client kubelib.Client) error {
	meshConfigMap, err := client.CoreV1().ConfigMaps(string(sa.istioNamespace)).Get(context.TODO(), meshConfigMapName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not read configmap %q from namespace %q: %v", meshConfigMapName, sa.istioNamespace, err)
	}

	configYaml, ok := meshConfigMap.Data[meshConfigMapKey]
	if !ok {
		return fmt.Errorf("missing config map key %q", meshConfigMapKey)
	}

	cfg, err := mesh.ApplyMeshConfigDefaults(configYaml)
	if err != nil {
		return fmt.Errorf("error parsing mesh config: %v", err)
	}

	sa.meshCfg = cfg

	meshNetworksYaml, ok := meshConfigMap.Data[meshNetworksMapKey]
	if !ok {
		return fmt.Errorf("missing config map key %q", meshNetworksMapKey)
	}

	mn, err := mesh.ParseMeshNetworks(meshNetworksYaml)
	if err != nil {
		return fmt.Errorf("error parsing mesh networks: %v", err)
	}

	sa.meshNetworks = mn
	return nil
}

// CollectionReporterFn is a hook function called whenever a collection is accessed through the AnalyzingDistributor's context
type CollectionReporterFn func(collection.Name)

// copied from processing/snapshotter/analyzingdistributor.go
func filterMessages(messages diag.Messages, namespaces map[resource.Namespace]struct{}, suppressions []AnalysisSuppression) diag.Messages {
	nsNames := make(map[string]struct{})
	for k := range namespaces {
		nsNames[k.String()] = struct{}{}
	}

	var msgs diag.Messages
FilterMessages:
	for _, m := range messages {
		// Only keep messages for resources in namespaces we want to analyze if the
		// message doesn't have an origin (meaning we can't determine the
		// namespace). Also kept are cluster-level resources where the namespace is
		// the empty string. If no such limit is specified, keep them all.
		if len(namespaces) > 0 && m.Resource != nil && m.Resource.Origin.Namespace() != "" {
			if _, ok := nsNames[m.Resource.Origin.Namespace().String()]; !ok {
				continue FilterMessages
			}
		}

		// Filter out any messages on resources with suppression annotations.
		if m.Resource != nil && m.Resource.Metadata.Annotations[annotation.GalleyAnalyzeSuppress.Name] != "" {
			for _, code := range strings.Split(m.Resource.Metadata.Annotations[annotation.GalleyAnalyzeSuppress.Name], ",") {
				if code == "*" || m.Type.Code() == code {
					scope.Analysis.Debugf("Suppressing code %s on resource %s due to resource annotation", m.Type.Code(), m.Resource.Origin.FriendlyName())
					continue FilterMessages
				}
			}
		}

		// Filter out any messages that match our suppressions.
		for _, s := range suppressions {
			if m.Resource == nil || s.Code != m.Type.Code() {
				continue
			}

			if !glob.Glob(s.ResourceName, m.Resource.Origin.FriendlyName()) {
				continue
			}
			scope.Analysis.Debugf("Suppressing code %s on resource %s due to suppressions list", m.Type.Code(), m.Resource.Origin.FriendlyName())
			continue FilterMessages
		}

		msgs = append(msgs, m)
	}
	return msgs
}

// AnalysisSuppression describes a resource and analysis code to be suppressed
// (e.g. ignored) during analysis. Used when a particular message code is to be
// ignored for a specific resource.
type AnalysisSuppression struct {
	// Code is the analysis code to suppress (e.g. "IST0104").
	Code string

	// ResourceName is the name of the resource to suppress the message for. For
	// K8s resources it has the same form as used by istioctl (e.g.
	// "DestinationRule default.istio-system"). Note that globbing wildcards are
	// supported (e.g. "DestinationRule *.istio-system").
	ResourceName string
}

// ReaderSource is a tuple of a io.Reader and filepath.
type ReaderSource struct {
	// Name is the name of the source (commonly the path to a file, but can be "-" for sources read from stdin or "" if completely synthetic).
	Name string
	// Reader is the reader instance to use.
	Reader io.Reader
}
