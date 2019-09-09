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

package sdscompare

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/istio/security/pkg/nodeagent/sds"
)

// SDSComparator diffs secrets between a config dump from target envoy and its corresponding node agent's debug endpoints
type SDSComparator struct {
	w              SDSWriter
	targetPod      string
	envoy          *configdump.Wrapper
	agentResponses map[string]sds.Debug
}

// NewSDSComparator generates an SDSComparator
func NewSDSComparator(
	w SDSWriter, nodeAgentResponses map[string]sds.Debug, envoyResponse []byte, targetPod string) (*SDSComparator, error) {
	envoyDump := &configdump.Wrapper{}
	err := json.Unmarshal(envoyResponse, envoyDump)
	if err != nil {
		return nil, err
	}
	c := &SDSComparator{
		w:              w,
		envoy:          envoyDump,
		agentResponses: nodeAgentResponses,
		targetPod:      targetPod,
	}
	return c, nil
}

// Diff will perform the diffing between node agent and envoy secrets, and display the results
func (c *SDSComparator) Diff() error {
	// retrieve node agent secrets that correspond to the target pod being examined
	agentSecrets, err := GetNodeAgentSecrets(c.agentResponses, func(connName string) bool {
		return strings.Contains(connName, c.targetPod)
	})
	if err != nil {
		return fmt.Errorf("could not parse node agent secrets: %v", err)
	}

	proxySecrets, err := GetEnvoySecrets(c.envoy)
	if err != nil {
		return fmt.Errorf("could not parse secrets from sidecar config dump: %v", err)
	}

	// compare the secrets based on their exact contents
	// if the raw data is the same, we consider the secret as identical
	secretMap := make(map[string]*SecretItemDiff)
	for _, secret := range proxySecrets {
		status := SecretItemDiff{
			Proxy:      secret.State,
			SecretItem: secret,
		}
		secretMap[secret.Data] = &status
	}

	for _, secret := range agentSecrets {
		match, contained := secretMap[secret.Data]
		if contained {
			match.Agent = "PRESENT"
			match.Source = secret.Source
		} else {
			status := SecretItemDiff{
				Agent:      "PRESENT",
				SecretItem: secret,
			}
			secretMap[secret.Data] = &status
		}
	}

	// now, the map has no use, as the values contain all the diff information
	resultDiffs := make([]SecretItemDiff, len(secretMap))
	i := 0
	for _, v := range secretMap {
		resultDiffs[i] = *v
		i++
	}

	sort.Slice(resultDiffs, func(i, j int) bool {
		return resultDiffs[i].State < resultDiffs[j].State
	})
	return c.w.PrintDiffs(resultDiffs)
}
