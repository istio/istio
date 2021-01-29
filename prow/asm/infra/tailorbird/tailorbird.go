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

package tailorbird

func tailorbirdDeployerBaseFlags() []string {
	return []string{"--down", "--tbenv=int", "--verbose"}
}

// DeployerFlags returns the deployer flags needed for the given cluster type,
// cluster topology and the feature to test
// TODO(chizhg): get the tailorbird config file based on the cluster type and
// cluster topology, rather than passing it through the extra deployer flag.
func DeployerFlags(clusterType, clusterTopology, featureToTest string) []string {
	return tailorbirdDeployerBaseFlags()
}
