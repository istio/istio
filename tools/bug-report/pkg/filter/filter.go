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

package filter

import (
	"fmt"

	"istio.io/istio/pkg/util/sets"
	cluster2 "istio.io/istio/tools/bug-report/pkg/cluster"
	"istio.io/istio/tools/bug-report/pkg/config"
	"istio.io/istio/tools/bug-report/pkg/util/match"
	"istio.io/istio/tools/bug-report/pkg/util/path"
)

// GetMatchingPaths returns a slice of matching paths, given a cluster tree and config.
// config is the capture configuration.
// cluster is the structure representing the cluster resource hierarchy.
func GetMatchingPaths(config *config.BugReportConfig, cluster *cluster2.Resources) ([]string, error) {
	paths, err := getMatchingPathsForSpec(config, cluster)
	if err != nil {
		return nil, err
	}
	return paths.SortedList(), nil
}

func getMatchingPathsForSpec(config *config.BugReportConfig, cluster *cluster2.Resources) (sets.Set, error) {
	return getMatchingPathsForSpecImpl(config, cluster, cluster.Root, nil, sets.New())
}

func getMatchingPathsForSpecImpl(config *config.BugReportConfig, cluster *cluster2.Resources, node map[string]any,
	path path.Path, matchingPaths sets.Set,
) (sets.Set, error) {
	for pe, n := range node {
		np := append(path, pe)
		if nn, ok := n.(map[string]any); ok {
			// non-leaf node
			mp, err := getMatchingPathsForSpecImpl(config, cluster, nn, np, matchingPaths)
			if err != nil {
				return nil, err
			}
			matchingPaths = matchingPaths.Union(mp)
			continue
		}
		// container name leaf
		cn, ok := n.(string)
		if !ok && n != nil {
			return nil, fmt.Errorf("bad node type at path %s: got %T, expected string", n, cn)
		}
		if len(np) != 4 {
			return nil, fmt.Errorf("unexpected leaf at path %s, expect leaf path to be namespace.deployment.pod.container", np)
		}
		if matchesKubeCaptureConfig(config, cluster, np[0], np[1], np[2], np[3]) {
			matchingPaths[np.String()] = struct{}{}
		}
	}
	return matchingPaths, nil
}

func matchesKubeCaptureConfig(config *config.BugReportConfig, cluster *cluster2.Resources, namespace, deployment, pod, container string) bool {
	for _, sp := range config.Exclude {
		if matchesSelectionSpec(sp, cluster, namespace, deployment, pod, container) {
			return false
		}
	}
	for _, sp := range parseIncluded(config.Include) {
		if matchesSelectionSpec(sp, cluster, namespace, deployment, pod, container) {
			return true
		}
	}
	return false
}

// matchesSelectionSpec reports whether the given container path is selected by any SelectionSpec.
func matchesSelectionSpec(sp *config.SelectionSpec, cluster *cluster2.Resources, namespace, deployment, pod, container string) bool {
	// For inclusion, match all if nothing is set.
	if !match.MatchesGlobs(namespace, sp.Namespaces) {
		return false
	}

	if !match.MatchesGlobs(deployment, sp.Deployments) {
		return false
	}

	if !match.MatchesGlobs(pod, sp.Pods) && len(sp.Pods) != 0 {
		return false
	}

	if !match.MatchesGlobs(container, sp.Containers) {
		return false
	}

	if !match.MatchesGlobs(container, sp.Containers) {
		return false
	}

	key := cluster2.PodKey(namespace, pod)
	if !match.MatchesMap(sp.Labels, cluster.Labels[key]) {
		return false
	}
	if !match.MatchesMap(sp.Annotations, cluster.Annotations[key]) {
		return false
	}
	return true
}

func parseIncluded(included []*config.SelectionSpec) []*config.SelectionSpec {
	if len(included) != 0 {
		return included
	}
	return []*config.SelectionSpec{
		{
			Namespaces: []string{"*"},
		},
	}
}
