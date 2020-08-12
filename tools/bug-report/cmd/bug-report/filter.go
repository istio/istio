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

package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"istio.io/istio/operator/pkg/util"
	cluster "istio.io/istio/tools/bug-report/pkg"
	"istio.io/pkg/log"
)

func parseIncluded(included []*SelectionSpec) []*SelectionSpec {
	if len(included) != 0 {
		return included
	}
	return []*SelectionSpec{
		{
			Namespaces: []string{"*"},
		},
	}
}

// GetMatchingPaths returns a slice of matching paths, given a cluster tree and config.
// config is the capture configuration.
// cluster is the structure representing the cluster resource hierarchy.
func GetMatchingPaths(config *KubeCaptureConfig, cluster *cluster.Resources) ([]string, error) {
	paths, err := getMatchingPathsForSpec(config, cluster)
	if err != nil {
		return nil, err
	}
	var out []string
	for p := range paths {
		out = append(out, p)
	}
	sort.Strings(out)

	return out, nil
}

func mergeMaps(a, b map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{})
	for k := range a {
		out[k] = struct{}{}
	}
	for k := range b {
		out[k] = struct{}{}
	}
	return out
}

func getMatchingPathsForSpec(config *KubeCaptureConfig, cluster *cluster.Resources) (map[string]struct{}, error) {
	paths := make(map[string]struct{})
	return getMatchingPathsForSpecImpl(config, cluster, cluster.Root, nil, paths)
}

func getMatchingPathsForSpecImpl(config *KubeCaptureConfig, cluster *cluster.Resources, node map[string]interface{}, path util.Path, matchingPaths map[string]struct{}) (map[string]struct{}, error) {
	for pe, n := range node {
		np := append(path, pe)
		if nn, ok := n.(map[string]interface{}); ok {
			// non-leaf node
			mp, err := getMatchingPathsForSpecImpl(config, cluster, nn, np, matchingPaths)
			if err != nil {
				return nil, err
			}
			matchingPaths = mergeMaps(matchingPaths, mp)
			continue
		}
		// container name leaf
		cn, ok := n.(string)
		if !ok && n != nil {
			return nil, fmt.Errorf("bad node type at path %s: got %T, expected string", np, cn)
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

func matchesKubeCaptureConfig(config *KubeCaptureConfig, cluster *cluster.Resources, namespace, deployment, pod, container string) bool {
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
func matchesSelectionSpec(sp *SelectionSpec, cluster *cluster.Resources, namespace, deployment, pod, container string) bool {
	// For inclusion, match all if nothing is set.
	if !matchesGlobs(namespace, sp.Namespaces) {
		return false
	}

	if !matchesGlobs(deployment, sp.Deployments) {
		return false
	}

	if !matchesGlobs(pod, sp.Pods) && len(sp.Pods) != 0 {
		return false
	}

	if !matchesGlobs(container, sp.Containers) {
		return false
	}

	if !matchesGlobs(container, sp.Containers) {
		return false
	}

	key := strings.Join([]string{namespace, deployment, pod}, ".")
	if !matchesMap(sp.Labels, cluster.Labels[key]) {
		return false
	}
	if !matchesMap(sp.Annotations, cluster.Annotations[key]) {
		return false
	}
	return true
}

func matchesMap(selection, cluster map[string]string) bool {
	if len(selection) == 0 {
		return true
	}
	if len(cluster) == 0 {
		return false
	}

	for ks, vs := range selection {
		vc, ok := cluster[ks]
		if !ok {
			return false
		}
		if !matchesGlob(vc, vs) {
			return false
		}
	}
	return true
}

func matchesGlobs(matchString string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	if len(patterns) == 1 {
		p := strings.TrimSpace(patterns[0])
		if p == "" || p == "*" {
			return true
		}
	}

	for _, p := range patterns {
		if matchesGlob(matchString, p) {
			return true
		}
	}
	return false
}

func matchesGlob(matchString, pattern string) bool {
	match, err := filepath.Match(pattern, matchString)
	if err != nil {
		// Shouldn't be here as prior validation is assumed.
		log.Errorf("Unexpected filepath error for %s match %s: %s", pattern, matchString, err)
		return false
	}
	return match
}
