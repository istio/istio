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

package analyzers

import (
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pilot/pkg/config/file"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/multicluster"
	"istio.io/istio/pkg/config/analysis/local"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
)

type mcTestCase struct {
	name               string
	cluster1InputFiles []string
	cluster2InputFiles []string
	analyzer           analysis.Analyzer
	expected           []message
}

var mcTestGrid = []mcTestCase{
	{
		name: "InconsistentMultiClusterService",
		cluster1InputFiles: []string{
			"testdata/multicluster/inconsistent-service-1.yaml",
		},
		cluster2InputFiles: []string{
			"testdata/multicluster/inconsistent-service-2.yaml",
		},
		analyzer: &multicluster.ServiceAnalyzer{},
		expected: []message{
			{msg.MultiClusterInconsistentService, "Service my-namespace/extra-port"},
			{msg.MultiClusterInconsistentService, "Service my-namespace/inconsistent-port-name"},
			{msg.MultiClusterInconsistentService, "Service my-namespace/mixed-mode"},
			{msg.MultiClusterInconsistentService, "Service my-namespace/mixed-type"},
			{msg.MultiClusterInconsistentService, "Service my-namespace/mixed-port-protocol"},
		},
	},
}

// TestMultiClusterAnalyzers sets up two clusters and runs the multi-cluster analyzers on them.
func TestMultiClusterAnalyzers(t *testing.T) {
	requestedInputsByAnalyzer := make(map[string]map[config.GroupVersionKind]struct{})
	// For each test case, verify we get the expected messages as output
	for _, tc := range mcTestGrid {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			// Set up a hook to record which collections are accessed by each analyzer
			analyzerName := tc.analyzer.Metadata().Name
			cr := func(col config.GroupVersionKind) {
				if _, ok := requestedInputsByAnalyzer[analyzerName]; !ok {
					requestedInputsByAnalyzer[analyzerName] = make(map[config.GroupVersionKind]struct{})
				}
				requestedInputsByAnalyzer[analyzerName][col] = struct{}{}
			}

			// Set up Analyzer for this test case
			sa, err := setupMultiClusterEnvironmentForCase(tc, cr)
			if err != nil {
				t.Fatalf("Error setting up analysis for testcase %s: %v", tc.name, err)
			}

			// Run the analysis
			result, err := runAnalyzer(sa)
			if err != nil {
				t.Fatalf("Error running analysis on testcase %s: %v", tc.name, err)
			}

			g.Expect(extractFields(result.Messages)).To(ConsistOf(tc.expected), "%v", prettyPrintMessages(result.Messages))
		})
	}
}

func setupMultiClusterEnvironmentForCase(tc mcTestCase, cr local.CollectionReporterFn) (*local.IstiodAnalyzer, error) {
	sa := local.NewIstiodAnalyzer(AllMultiClusterCombined(), "", "istio-system", cr)

	// Add the test files to the fake client
	if err := addStore(sa, "cluster1", tc.cluster1InputFiles); err != nil {
		return nil, err
	}
	if err := addStore(sa, "cluster2", tc.cluster2InputFiles); err != nil {
		return nil, err
	}

	// Include default resources
	if err := sa.AddDefaultResources(); err != nil {
		return nil, fmt.Errorf("error adding default resources: %v", err)
	}
	return sa, nil
}

type fakeClientImpl struct {
	kube.CLIClient
	clusterID cluster.ID
}

func (f *fakeClientImpl) ClusterID() cluster.ID {
	return f.clusterID
}

func addStore(sa *local.IstiodAnalyzer, clusterName string, yamls []string) error {
	client := &fakeClientImpl{
		CLIClient: kube.NewFakeClient(),
		clusterID: cluster.ID(clusterName),
	}
	// Gather test files
	src := file.NewKubeSource(collections.All)
	for i, yamlFile := range yamls {
		data, err := os.ReadFile(yamlFile)
		if err != nil {
			return err
		}
		err = src.ApplyContent(fmt.Sprintf("%d", i), string(data))
		if err != nil {
			return err
		}
	}
	sa.AddSourceForCluster(src, cluster.ID(clusterName))
	sa.AddRunningKubeSourceWithRevision(client, clusterName, false)
	return nil
}
