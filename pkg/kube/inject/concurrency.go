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

package inject

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"istio.io/pkg/log"
)

const (
	// concurrencyCmdFlagName
	concurrencyCmdFlagName = "concurrency"
)

var (
	// regex pattern for to extract the pilot agent concurrency.
	// Supported format, --concurrency, -concurrency, --concurrency=2.
	concurrencyPattern = regexp.MustCompile(fmt.Sprintf(`^-{1,2}%s(=(?P<threads>\d+))?$`, concurrencyCmdFlagName))
)

// extractConcurrency accepts the sidecar container spec and returns its concurrency.
func extractConcurrency(sidecar *corev1.Container) int {
	for i, arg := range sidecar.Args {
		// Skip for unrelated args.
		match := concurrencyPattern.FindAllStringSubmatch(strings.TrimSpace(arg), -1)
		if len(match) != 1 {
			continue
		}
		groups := concurrencyPattern.SubexpNames()
		concurrency := ""
		for ind, s := range match[0] {
			if groups[ind] == "threads" {
				concurrency = s
				break
			}
		}
		// concurrency not found from current arg, extract from next arg.
		if concurrency == "" {
			// Matches the regex pattern, but without actual values provided.
			if len(sidecar.Args) <= i+1 {
				return 0
			}
			concurrency = sidecar.Args[i+1]
		}
		c, err := strconv.Atoi(concurrency)
		if err != nil {
			log.Errorf("Failed to convert concurrency to int %v, err %v", concurrency, err)
			return 0
		}
		return c
	}
	return 0
}

// applyConcurrency changes sidecar containers' concurrency to equals the cpu cores of the container
// if not set. It is inferred from the container's resource limit or request.
func applyConcurrency(containers []corev1.Container) {
	for i, c := range containers {
		if c.Name == ProxyContainerName {
			concurrency := extractConcurrency(&c)
			// do not change it when it is already set
			if concurrency > 0 {
				return
			}

			// firstly use cpu limits
			if !updateConcurrency(&containers[i], c.Resources.Limits.Cpu().MilliValue()) {
				// secondly use cpu requests
				updateConcurrency(&containers[i], c.Resources.Requests.Cpu().MilliValue())
			}
			return
		}
	}
}

func updateConcurrency(container *corev1.Container, cpumillis int64) bool {
	cpu := float64(cpumillis) / 1000
	concurrency := int(math.Ceil(cpu))
	if concurrency > 0 {
		container.Args = append(container.Args, []string{fmt.Sprintf("--%s", concurrencyCmdFlagName), strconv.Itoa(concurrency)}...)
		return true
	}

	return false
}
