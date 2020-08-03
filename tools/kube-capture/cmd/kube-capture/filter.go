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

	"istio.io/istio/operator/pkg/util"
	cluster "istio.io/istio/tools/kube-capture/pkg"
	"istio.io/pkg/log"
)

// GetMatchingPaths returns a slice of matching paths, given a cluster tree and config.
// config is the capture configuration.
// cluster is the structure representing the cluster resource hierarchy.
func GetMatchingPaths(config *KubeCaptureConfig, cluster *cluster.Resources) ([]string, error) {
	paths := make(map[string]struct{})
	for _, sp := range config.Included {
		np, err := getMatchingPathsForSpec(config, cluster, sp)
		if err != nil {
			return nil, err
		}
		paths = mergeMaps(paths, np)
	}
	for _, sp := range config.Excluded {
		np, err := getMatchingPathsForSpec(config, cluster, sp)
		if err != nil {
			return nil, err
		}
		for p := range np {
			delete(paths, p)
		}
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

func getMatchingPathsForSpec(config *KubeCaptureConfig, cluster *cluster.Resources, sp *SelectionSpec) (map[string]struct{}, error) {
	paths := make(map[string]struct{})
	return getMatchingPathsForSpecImpl(config, cluster, sp, cluster.Root, nil, paths)
}

func getMatchingPathsForSpecImpl(config *KubeCaptureConfig, cluster *cluster.Resources, sp *SelectionSpec, node map[string]interface{}, path util.Path, matchingPaths map[string]struct{}) (map[string]struct{}, error) {
	for pe, n := range node {
		np := append(path, pe)
		if nn, ok := n.(map[string]interface{}); ok {
			// non-leaf node
			return getMatchingPathsForSpecImpl(config, cluster, sp, nn, np, matchingPaths)
		}
		// container name leaf
		cn, ok := n.(string)
		if !ok {
			return nil, fmt.Errorf("bad node type at path %s: got %T, expected string", np, cn)
		}
		if len(np) != 4 {
			return nil, fmt.Errorf("unexpected leaf at path %s, expect leaf path to be namespace.deployment.pod.container", np)
		}
		if matchesKubeCaptureConfig(config, np[0], np[1], np[2], np[3]) {
			matchingPaths[np.String()] = struct{}{}
		}
	}
	return matchingPaths, nil
}

func matchesKubeCaptureConfig(config *KubeCaptureConfig, namespace, deployment, pod, container string) bool {
	for _, sp := range config.Excluded {
		if matchesSelectionSpec(sp, namespace, deployment, pod, container) {
			return false
		}
	}
	for _, sp := range config.Included {
		if matchesSelectionSpec(sp, namespace, deployment, pod, container) {
			return true
		}
	}
	return false
}

// matchesSelectionSpec reports whether the given container path is selected by any SelectionSpec.
func matchesSelectionSpec(sp *SelectionSpec, namespace, deployment, pod, container string) bool {
	if matchesGlobs(namespace, sp.Namespaces[Exclude]) {
		return false
	}
	if !matchesGlobs(namespace, sp.Namespaces[Include]) {
		return false
	}
	if matchesGlobs(deployment, sp.Deployments[Exclude]) {
		return false
	}
	if !matchesGlobs(deployment, sp.Deployments[Include]) {
		return false
	}
	if matchesGlobs(pod, sp.Pods[Exclude]) {
		return false
	}
	if !matchesGlobs(pod, sp.Pods[Include]) {
		return false
	}
	if matchesGlobs(container, sp.Containers[Exclude]) {
		return false
	}
	if !matchesGlobs(container, sp.Containers[Include]) {
		return false
	}
	return true
}

func matchesGlobs(matchString string, globs []string) bool {
	for _, g := range globs {
		match, err := filepath.Match(matchString, g)
		if err != nil {
			// Shouldn't be here as prior validation is assumed.
			log.Errorf("Unexpected filepath error for %s match %s: %s", matchString, g, err)
			return false
		}
		if match {
			return true
		}
	}
	return false
}
