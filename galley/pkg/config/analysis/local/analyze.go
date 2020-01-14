// Copyright 2019 Istio Authors
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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	"github.com/hashicorp/go-multierror"

	authorizationapi "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/mesh/v1alpha1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/meshcfg"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/galley/pkg/config/processor"
	"istio.io/istio/galley/pkg/config/processor/transforms"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/schema"
	"istio.io/istio/galley/pkg/config/schema/collection"
	"istio.io/istio/galley/pkg/config/schema/collections"
	"istio.io/istio/galley/pkg/config/schema/snapshots"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/source/kube/inmemory"
	"istio.io/istio/galley/pkg/config/util/kuberesource"
	"istio.io/istio/pkg/config/mesh"
)

const (
	domainSuffix      = "cluster.local"
	meshConfigMapKey  = "mesh"
	meshConfigMapName = "istio"
)

// Pseudo-constants, since golang doesn't support a true const slice/array
var (
	requiredPerms = []string{"list", "watch"}
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

	// Which kube resources are used by this analyzer
	// Derived from metadata and the specified analyzer and transformer providers
	kubeResources collection.Schemas

	// Hook function called when a collection is used in analysis
	collectionReporter snapshotter.CollectionReporterFn
}

// AnalysisResult represents the returnable results of an analysis execution
type AnalysisResult struct {
	Messages          diag.Messages
	SkippedAnalyzers  []string
	ExecutedAnalyzers []string
}

// NewSourceAnalyzer creates a new SourceAnalyzer with no sources. Use the Add*Source methods to add sources in ascending precedence order,
// then execute Analyze to perform the analysis
func NewSourceAnalyzer(m *schema.Metadata, analyzer *analysis.CombinedAnalyzer, namespace, istioNamespace resource.Namespace,
	cr snapshotter.CollectionReporterFn, serviceDiscovery bool) *SourceAnalyzer {

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
		meshCfg:              meshcfg.Default(),
		sources:              make([]precedenceSourceInput, 0),
		analyzer:             analyzer,
		transformerProviders: transformerProviders,
		namespace:            namespace,
		istioNamespace:       istioNamespace,
		kubeResources:        kubeResources,
		collectionReporter:   cr,
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

	// Create a source representing mesh config. There should be exactly one of these.
	meshsrc := meshcfg.NewInmemory()
	meshsrc.Set(sa.meshCfg)
	sa.sources = append(sa.sources, precedenceSourceInput{
		src: meshsrc,
		cols: collection.Names{
			collections.IstioMeshV1Alpha1MeshConfig.Name(),
		},
	})

	var namespaces []resource.Namespace
	if sa.namespace != "" {
		namespaces = []resource.Namespace{sa.namespace}
	}

	var colsInSnapshots collection.Names
	for _, c := range sa.m.AllCollectionsInSnapshots([]string{snapshots.LocalAnalysis, snapshots.SyntheticServiceEntry}) {
		colsInSnapshots = append(colsInSnapshots, collection.NewName(c))
	}

	result.SkippedAnalyzers = sa.analyzer.RemoveSkipped(colsInSnapshots, sa.kubeResources.DisabledCollectionNames(),
		sa.transformerProviders)
	result.ExecutedAnalyzers = sa.analyzer.AnalyzerNames()

	updater := &snapshotter.InMemoryStatusUpdater{}
	distributorSettings := snapshotter.AnalyzingDistributorSettings{
		StatusUpdater:      updater,
		Analyzer:           sa.analyzer,
		Distributor:        snapshotter.NewInMemoryDistributor(),
		AnalysisSnapshots:  []string{snapshots.LocalAnalysis, snapshots.SyntheticServiceEntry},
		TriggerSnapshot:    snapshots.LocalAnalysis,
		CollectionReporter: sa.collectionReporter,
		AnalysisNamespaces: namespaces,
		Suppressions:       sa.suppressions,
	}
	distributor := snapshotter.NewAnalyzingDistributor(distributorSettings)

	processorSettings := processor.Settings{
		Metadata:           sa.m,
		DomainSuffix:       domainSuffix,
		Source:             newPrecedenceSource(sa.sources),
		TransformProviders: sa.transformerProviders,
		Distributor:        distributor,
		EnabledSnapshots:   []string{snapshots.LocalAnalysis, snapshots.SyntheticServiceEntry},
	}
	rt, err := processor.Initialize(processorSettings)
	if err != nil {
		return result, err
	}
	rt.Start()
	defer rt.Stop()

	scope.Analysis.Debugf("Waiting for analysis messages to be available...")
	if updater.WaitForReport(cancel) {
		result.Messages = updater.Get()
		return result, nil
	}

	return result, errors.New("cancelled")
}

// SetSuppressions will set the list of suppressions for the analyzer. Any
// resource that matches the provided suppression will not be included in the
// final message output.
func (sa *SourceAnalyzer) SetSuppressions(suppressions []snapshotter.AnalysisSuppression) {
	sa.suppressions = suppressions
}

// AddReaderKubeSource adds a source based on the specified k8s yaml files to the current SourceAnalyzer
func (sa *SourceAnalyzer) AddReaderKubeSource(readers []io.Reader) error {
	src := inmemory.NewKubeSource(sa.kubeResources)
	src.SetDefaultNamespace(sa.namespace)

	var errs error

	// If we encounter any errors reading or applying files, track them but attempt to continue
	for i, r := range readers {
		by, err := ioutil.ReadAll(r)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}

		if err = src.ApplyContent(string(i), string(by)); err != nil {
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

	// Since we're using a running k8s source, do a permissions pre-check and disable any resources the current user doesn't have permissions for
	sa.disableKubeResourcesWithoutPermissions(client)

	// Since we're using a running k8s source, try to get mesh config from the configmap
	if err := sa.addRunningKubeMeshConfigSource(client); err != nil {
		scope.Analysis.Errorf("error getting mesh config from running kube source: %v", err)
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

// AddDefaultResources adds some basic dummy Istio resources, based on mesh configuration.
// This is useful for files-only analysis cases where we don't expect the user to be including istio system resources
// and don't want to generate false positives because they aren't there.
// Respect mesh config when deciding which default resources should be generated
func (sa *SourceAnalyzer) AddDefaultResources() error {
	var readers []io.Reader

	if sa.meshCfg.GetIngressControllerMode() != v1alpha1.MeshConfig_OFF {
		ingressResources, err := getDefaultIstioIngressGateway(sa.istioNamespace.String(), sa.meshCfg.GetIngressService())
		if err != nil {
			return err
		}
		readers = append(readers, strings.NewReader(ingressResources))
	}

	if len(readers) == 0 {
		return nil
	}

	return sa.AddReaderKubeSource(readers)
}

func (sa *SourceAnalyzer) addRunningKubeMeshConfigSource(client kubernetes.Interface) error {
	meshConfigMap, err := client.CoreV1().ConfigMaps(string(sa.istioNamespace)).Get(meshConfigMapName, metav1.GetOptions{})
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
	return nil
}

func (sa *SourceAnalyzer) disableKubeResourcesWithoutPermissions(client kubernetes.Interface) {
	resultBuilder := collection.NewSchemasBuilder()

	for _, s := range sa.kubeResources.All() {
		if !s.IsDisabled() {
			allowed, err := hasPermissionsOnCollection(client, s, requiredPerms)
			if err != nil {
				scope.Analysis.Errorf("Error checking permissions for resource %q (skipping it): %v", s.Resource().CanonicalName(), err)
				s = s.Disable()
			} else if !allowed {
				scope.Analysis.Errorf("Skipping resource %q since the current user doesn't have required permissions %v",
					s.Resource().CanonicalName(), requiredPerms)
				s = s.Disable()
			}
		}

		// The possible error here is if the collection is already in the list.
		// Since we are making a clone of the list with modified elements,
		// we can be sure this won't happen and safely ignore the returned error.
		_ = resultBuilder.Add(s)
	}

	sa.kubeResources = resultBuilder.Build()
}

func hasPermissionsOnCollection(client kubernetes.Interface, s collection.Schema, verbs []string) (bool, error) {
	for _, verb := range verbs {
		sar := &authorizationapi.SelfSubjectAccessReview{
			Spec: authorizationapi.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationapi.ResourceAttributes{
					Verb:     verb,
					Group:    s.Resource().Group(),
					Resource: s.Resource().CanonicalName(),
				},
			},
		}

		response, err := client.AuthorizationV1().SelfSubjectAccessReviews().Create(sar)
		if err != nil {
			return false, fmt.Errorf("error creating SelfSubjectAccessReview: %v", err)
		}

		if !response.Status.Allowed {
			return false, nil
		}
	}
	return true, nil
}
