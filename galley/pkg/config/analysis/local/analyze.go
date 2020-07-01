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
	"io/ioutil"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/mesh/v1alpha1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	galley_mesh "istio.io/istio/galley/pkg/config/mesh"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/galley/pkg/config/processor"
	"istio.io/istio/galley/pkg/config/processor/transforms"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/source/kube/inmemory"
	"istio.io/istio/galley/pkg/config/util/kuberesource"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/snapshots"
)

const (
	meshConfigMapKey   = "mesh"
	meshConfigMapName  = "istio"
	meshNetworksMapKey = "meshNetworks"
)

// Pseudo-constants, since golang doesn't support a true const slice/array
var (
	analysisSnapshots = []string{snapshots.LocalAnalysis}
)

// Patch table
var (
	apiserverNew = apiserver.New
)

// SourceAnalyzer handles local analysis of k8s event sources, both live and file-based
type SourceAnalyzer struct {
	m                    *schema.Metadata
	sources              []precedenceSourceInput
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
	collectionReporter snapshotter.CollectionReporterFn

	// How long to wait for snapshot + analysis to complete before aborting
	timeout time.Duration
}

// AnalysisResult represents the returnable results of an analysis execution
type AnalysisResult struct {
	Messages          diag.Messages
	SkippedAnalyzers  []string
	ExecutedAnalyzers []string
}

// ReaderSource is a tuple of a io.Reader and filepath.
type ReaderSource struct {
	// Name is the name of the source (commonly the path to a file, but can be "-" for sources read from stdin or "" if completely synthetic).
	Name string
	// Reader is the reader instance to use.
	Reader io.Reader
}

// NewSourceAnalyzer creates a new SourceAnalyzer with no sources. Use the Add*Source methods to add sources in ascending precedence order,
// then execute Analyze to perform the analysis
func NewSourceAnalyzer(m *schema.Metadata, analyzer *analysis.CombinedAnalyzer, namespace, istioNamespace resource.Namespace,
	cr snapshotter.CollectionReporterFn, serviceDiscovery bool, timeout time.Duration) *SourceAnalyzer {

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

	sa := &SourceAnalyzer{
		m:                    m,
		meshCfg:              galley_mesh.DefaultMeshConfig(),
		meshNetworks:         galley_mesh.DefaultMeshNetworks(),
		sources:              make([]precedenceSourceInput, 0),
		analyzer:             analyzer,
		transformerProviders: transformerProviders,
		namespace:            namespace,
		istioNamespace:       istioNamespace,
		kubeResources:        kubeResources,
		collectionReporter:   cr,
		timeout:              timeout,
	}

	return sa
}

// Analyze loads the sources and executes the analysis
func (sa *SourceAnalyzer) Analyze(cancel chan struct{}) (AnalysisResult, error) {
	var result AnalysisResult

	// We need at least one non-meshcfg source
	if len(sa.sources) == 0 {
		return result, fmt.Errorf("at least one file and/or Kubernetes source must be provided")
	}

	// Create a source representing mesh config. There should be exactly one.
	meshconfig := galley_mesh.NewInmemoryMeshCfg()
	meshconfig.Set(sa.meshCfg)
	sa.sources = append(sa.sources, precedenceSourceInput{
		src: meshconfig,
		cols: collection.Names{
			collections.IstioMeshV1Alpha1MeshConfig.Name(),
		},
	})

	// Create a source representing meshnetworks. There should be exactly one.
	meshnetworks := galley_mesh.NewInmemoryMeshNetworks()
	meshnetworks.Set(sa.meshNetworks)
	sa.sources = append(sa.sources, precedenceSourceInput{
		src: meshnetworks,
		cols: collection.Names{
			collections.IstioMeshV1Alpha1MeshNetworks.Name(),
		},
	})

	var namespaces []resource.Namespace
	if sa.namespace != "" {
		namespaces = []resource.Namespace{sa.namespace}
	}

	var colsInSnapshots collection.Names
	for _, c := range sa.m.AllCollectionsInSnapshots(analysisSnapshots) {
		colsInSnapshots = append(colsInSnapshots, collection.NewName(c))
	}

	result.SkippedAnalyzers = sa.analyzer.RemoveSkipped(colsInSnapshots, sa.kubeResources.DisabledCollectionNames(),
		sa.transformerProviders)
	result.ExecutedAnalyzers = sa.analyzer.AnalyzerNames()

	updater := &snapshotter.InMemoryStatusUpdater{
		WaitTimeout: sa.timeout,
	}

	distributorSettings := snapshotter.AnalyzingDistributorSettings{
		StatusUpdater:      updater,
		Analyzer:           sa.analyzer,
		Distributor:        snapshotter.NewInMemoryDistributor(),
		AnalysisSnapshots:  analysisSnapshots,
		TriggerSnapshot:    snapshots.LocalAnalysis,
		CollectionReporter: sa.collectionReporter,
		AnalysisNamespaces: namespaces,
		Suppressions:       sa.suppressions,
	}
	distributor := snapshotter.NewAnalyzingDistributor(distributorSettings)

	processorSettings := processor.Settings{
		Metadata:           sa.m,
		DomainSuffix:       constants.DefaultKubernetesDomain,
		Source:             newPrecedenceSource(sa.sources),
		TransformProviders: sa.transformerProviders,
		Distributor:        distributor,
		EnabledSnapshots:   analysisSnapshots,
	}
	rt, err := processor.Initialize(processorSettings)
	if err != nil {
		return result, err
	}

	rt.Start()

	scope.Analysis.Debugf("Waiting for analysis messages to be available...")
	if err := updater.WaitForReport(cancel); err != nil {
		return result, fmt.Errorf("failed to get analysis result: %v", err)
	}

	result.Messages = updater.Get()

	rt.Stop()

	return result, nil
}

// SetSuppressions will set the list of suppressions for the analyzer. Any
// resource that matches the provided suppression will not be included in the
// final message output.
func (sa *SourceAnalyzer) SetSuppressions(suppressions []snapshotter.AnalysisSuppression) {
	sa.suppressions = suppressions
}

// AddReaderKubeSource adds a source based on the specified k8s yaml files to the current SourceAnalyzer
func (sa *SourceAnalyzer) AddReaderKubeSource(readers []ReaderSource) error {
	src := inmemory.NewKubeSource(sa.kubeResources)
	src.SetDefaultNamespace(sa.namespace)

	var errs error

	// If we encounter any errors reading or applying files, track them but attempt to continue
	for _, r := range readers {
		by, err := ioutil.ReadAll(r.Reader)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}

		if err = src.ApplyContent(r.Name, string(by)); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	sa.sources = append(sa.sources, precedenceSourceInput{src: src, cols: sa.kubeResources.CollectionNames()})

	return errs
}

// AddRunningKubeSource adds a source based on a running k8s cluster to the current SourceAnalyzer
// Also tries to get mesh config from the running cluster, if it can
func (sa *SourceAnalyzer) AddRunningKubeSource(k kube.Interfaces) {
	client, err := k.KubeClient()
	if err != nil {
		scope.Analysis.Errorf("error getting KubeClient: %v", err)
		return
	}

	// Since we're using a running k8s source, try to get meshconfig and meshnetworks from the configmap.
	if err := sa.addRunningKubeIstioConfigMapSource(client); err != nil {
		_, err := client.CoreV1().Namespaces().Get(context.TODO(), sa.istioNamespace.String(), metav1.GetOptions{})
		if kerrors.IsNotFound(err) {
			// An AnalysisMessage already show up to warn the absence of istio-system namespace, so making it debug level.
			scope.Analysis.Debugf("%v namespace not found. Istio may not be installed in the target cluster. "+
				"Using default mesh configuration values for analysis", sa.istioNamespace.String())
		} else if err != nil {
			scope.Analysis.Errorf("error getting mesh config from running kube source: %v", err)
		}
	}

	src := apiserverNew(apiserver.Options{
		Client:  k,
		Schemas: sa.kubeResources,
	})
	sa.sources = append(sa.sources, precedenceSourceInput{src: src, cols: sa.kubeResources.CollectionNames()})
}

// AddFileKubeMeshConfig gets mesh config from the specified yaml file
func (sa *SourceAnalyzer) AddFileKubeMeshConfig(file string) error {
	by, err := ioutil.ReadFile(file)
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
func (sa *SourceAnalyzer) AddFileKubeMeshNetworks(file string) error {
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
func (sa *SourceAnalyzer) AddDefaultResources() error {
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

func (sa *SourceAnalyzer) addRunningKubeIstioConfigMapSource(client kubernetes.Interface) error {
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
