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
	"os"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/ryanuber/go-glob"
	"istio.io/api/annotation"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/config/monitor"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/galley/pkg/config/analysis"
	galley_mesh "istio.io/istio/galley/pkg/config/mesh"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/galley/pkg/config/processor/transforms"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/util/kuberesource"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// IstiodAnalyzer handles local analysis of k8s event sources, both live and file-based
type IstiodAnalyzer struct {
	m                    *schema.Metadata
	//sources              []precedenceSourceInput
	internalStore  model.ConfigStore
	stores []model.ConfigStoreCache
	analyzer             *analysis.CombinedAnalyzer
	transformerProviders transformer.Providers
	namespace            resource.Namespace
	istioNamespace       resource.Namespace

	// List of code and resource suppressions to exclude messages on
	suppressions []snapshotter.AnalysisSuppression

	// Mesh config for this analyzer. This can come from multiple sources, and the last added version will take precedence.
	meshCfg *v1alpha1.MeshConfig

	// Mesh networks config for this analyzer.
	meshNetworks *v1alpha1.MeshNetworks

	// Which kube resources are used by this analyzer
	// Derived from metadata and the specified analyzer and transformer providers
	kubeResources collection.Schemas

	// Hook function called when a collection is used in analysis
	collectionReporter CollectionReporterFn

	// How long to wait for snapshot + analysis to complete before aborting
	timeout time.Duration
}

// NewIstiodAnalyzer creates a new IstiodAnalyzer with no sources. Use the Add*Source methods to add sources in ascending precedence order,
// then execute Analyze to perform the analysis
func NewIstiodAnalyzer(m *schema.Metadata, analyzer *analysis.CombinedAnalyzer, namespace, istioNamespace resource.Namespace,
	cr CollectionReporterFn, serviceDiscovery bool, timeout time.Duration) *IstiodAnalyzer {
	// collectionReporter hook function defaults to no-op
	if cr == nil {
		cr = func(collection.Name) {}
	}

	transformerProviders := transforms.Providers(m)

	// Get the closure of all input collections for our analyzer, paying attention to transforms
	kubeResources := kuberesource.DisableExcludedCollections(
		m.KubeCollections(),
		transformerProviders,
		analyzer.Metadata().Inputs,
		kuberesource.DefaultExcludedResourceKinds(),
		serviceDiscovery)

	sa := &IstiodAnalyzer{
		m:                    m,
		meshCfg:              galley_mesh.DefaultMeshConfig(),
		meshNetworks:         galley_mesh.DefaultMeshNetworks(),
		analyzer:             analyzer,
		transformerProviders: transformerProviders,
		namespace:            namespace,
		internalStore:        memory.Make(collections.All),
		istioNamespace:       istioNamespace,
		kubeResources:        kubeResources,
		collectionReporter:   cr,
		timeout:              timeout,
	}

	return sa
}

// Analyze loads the sources and executes the analysis
func (sa *IstiodAnalyzer) Analyze(cancel chan struct{}) (AnalysisResult, error) {
	var result AnalysisResult

	// We need at least one non-meshcfg source
	if len(sa.stores) == 0 {
		return result, fmt.Errorf("at least one file and/or Kubernetes source must be provided")
	}

	// TODO: there's gotta be a better way to convert v1meshconfig to config.Config...
	// Create a store containing mesh config. There should be exactly one.
	_, err := sa.internalStore.Create(config.Config{
		Meta:   config.Meta{
			GroupVersionKind: collections.IstioMeshV1Alpha1MeshConfig.Resource().GroupVersionKind(),
		},
		Spec:   sa.meshCfg,
	})
	// Create a store containing meshnetworks. There should be exactly one.
	_, err = sa.internalStore.Create(config.Config{
		Meta:   config.Meta{
			GroupVersionKind: collections.IstioMeshV1Alpha1MeshNetworks.Resource().GroupVersionKind(),
		},
		Spec:   sa.meshNetworks,
	})
	sa.stores = append(sa.stores, dfCache{ConfigStore: sa.internalStore})

	store, err := aggregate.MakeWriteableCache(sa.stores, nil)
	if err != nil {
		return result, err
	}

	result.SkippedAnalyzers = sa.analyzer.RemoveSkipped(store.Schemas().CollectionNames(), sa.kubeResources.DisabledCollectionNames(),
		sa.transformerProviders)
	result.ExecutedAnalyzers = sa.analyzer.AnalyzerNames()

	cache.WaitForCacheSync(cancel,
		store.HasSynced)
	ctx := &istiodContext{
		store:              store,
		cancelCh:           cancel,
		messages:           diag.Messages{},
		collectionReporter: sa.collectionReporter,
	}
	sa.analyzer.Analyze(ctx)

	namespaces := make(map[resource.Namespace]struct{})
	if sa.namespace != "" {
		namespaces[sa.namespace] = struct{}{}
	}
	// TODO: analysis is run for all namespaces, even if they are requested to be filtered.
	msgs := filterMessages(ctx.messages, namespaces, sa.suppressions)
	result.Messages = msgs

	return result, nil
}

type dfCache struct {
	model.ConfigStore
}

func (d dfCache) RegisterEventHandler(kind config.GroupVersionKind, handler model.EventHandler) {
	panic("implement me")
}

func (d dfCache) Run(stop <-chan struct{}) {
	panic("implement me")
}

func (d dfCache) SetWatchErrorHandler(f func(r *cache.Reflector, err error)) error {
	panic("implement me")
}

func (d dfCache) HasSynced() bool {
	panic("implement me")
}

// SetSuppressions will set the list of suppressions for the analyzer. Any
// resource that matches the provided suppression will not be included in the
// final message output.
func (sa *IstiodAnalyzer) SetSuppressions(suppressions []snapshotter.AnalysisSuppression) {
	sa.suppressions = suppressions
}

// AddReaderKubeSource adds a source based on the specified k8s yaml files to the current IstiodAnalyzer
func (sa *IstiodAnalyzer) AddReaderKubeSource(readers []ReaderSource) error {
	var errs error
	// TODO: seems like cluster.local should be a constant somewhere?  Is this always correct?
	rs := monitor.NewReaderSnapshot(collections.All, "cluster.local")
	for _, r := range readers {
		configs, err := rs.ReadConfigs(r.Reader)
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed reading %s: %s", r.Name, err))
			continue
		}
		for _, cfg := range configs {
			_, err = sa.internalStore.Create(*cfg)
			if err != nil {
				errs = multierror.Append(errs, fmt.Errorf("failed storing %s: %s", cfg.Name, err))
				continue
			}
		}
	}
	return errs
}

// AddRunningKubeSource adds a source based on a running k8s cluster to the current IstiodAnalyzer
// Also tries to get mesh config from the running cluster, if it can
func (sa *IstiodAnalyzer) AddRunningKubeSource(c kubelib.Client) {

	// TODO: are either of these string constants intended to vary?
	store, err := crdclient.New(c, "default", "cluster.local")
	sa.stores = append(sa.stores, store)
	if err != nil {
		scope.Analysis.Errorf("error adding KubeClient: %v", err)
		return
	}

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
		readers = append(readers, ReaderSource{Reader: strings.NewReader(ingressResources)})
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

type istiodContext struct {
	store							 model.ConfigStore
	cancelCh           chan struct{}
	messages           diag.Messages
	collectionReporter CollectionReporterFn
}

func (i *istiodContext) Report(c collection.Name, m diag.Message) {
	i.messages.Add(m)
}

func (i *istiodContext) Find(col collection.Name, name resource.FullName) *resource.Instance {
	i.collectionReporter(col)
	colschema, ok := collections.All.Find(col.String())
	if !ok {
		// TODO: demote this log before merging
		log.Errorf("collection %s could not be found", col.String())
		return nil
	}
	cfg := i.store.Get(colschema.Resource().GroupVersionKind(), name.Name.String(), name.Namespace.String())
	if cfg == nil {
		// TODO: demote this log before merging
		log.Errorf("collection %s does not have a member named", col.String(), name)
		return nil
	}
	mcpr, err := config.ConfigToResource(cfg)
	if err != nil {
		// TODO: demote this log before merging
		log.Errorf("failed converting cfg %s to mcp resource: %s", cfg.Name, err)
		return nil
	}
	result, err := resource.Deserialize(mcpr, colschema.Resource())
	if err != nil {
		// TODO: demote this log before merging
		log.Errorf("failed desirializing mcp resource %s to instance: %s", cfg.Name, err)
		return nil
	}
	return result
}

func (i *istiodContext) Exists(col collection.Name, name resource.FullName) bool {
	i.collectionReporter(col)
	return i.Find(col, name) != nil
}

func (i *istiodContext) ForEach(col collection.Name, fn analysis.IteratorFn) {
	i.collectionReporter(col)
	colschema, ok := collections.All.Find(col.String())
	if !ok {
		// TODO: demote this log before merging
		log.Errorf("collection %s could not be found", col.String())
		return
	}
	cfgs, err := i.store.List(colschema.Resource().GroupVersionKind(), "")
	if err != nil {
		// TODO: demote this log before merging
		log.Errorf("collection %s could not be listed: %s", col.String(), err)
		return
	}
	for _, cfg := range cfgs {
		mcpr, err := config.ConfigToResource(&cfg)
		if err != nil {
			// TODO: demote this log before merging
			log.Errorf("failed converting cfg %s to mcp resource: %s", cfg.Name, err)
			// TODO: is continuing the right thing here?
			continue
		}
		res, err := resource.Deserialize(mcpr, colschema.Resource())
		if err != nil {
			// TODO: demote this log before merging
			log.Errorf("failed desirializing mcp resource %s to instance: %s", cfg.Name, err)
			// TODO: is continuing the right thing here?
			continue
		}
		if !fn(res) {
			break
		}
	}
}

func (i *istiodContext) Canceled() bool {
	select {
	case <-i.cancelCh:
		return true
	default:
		return false
	}
}
// copied from processing/snapshotter/analyzingdistributor.go
func filterMessages(messages diag.Messages, namespaces map[resource.Namespace]struct{}, suppressions []snapshotter.AnalysisSuppression) diag.Messages {
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
