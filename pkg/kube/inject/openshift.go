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

// Functions below were copied from
// https://github.com/openshift/apiserver-library-go/blob/c22aa58bb57416b9f9f190957d07c9e7669c26df/pkg/securitycontextconstraints/sccmatching/matcher.go
// These functions are not exported, and, if they were, when imported bring k8s.io/kubernetes as dependency, which is problematic
// License is Apache 2.0: https://github.com/openshift/apiserver-library-go/blob/c22aa58bb57416b9f9f190957d07c9e7669c26df/LICENSE

package inject

import (
	"fmt"
	"strings"

	securityv1 "github.com/openshift/api/security/v1"
	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/log"
)

// getPreallocatedUIDRange retrieves the annotated value from the namespace, splits it to make
// the min/max and formats the data into the necessary types for the strategy options.
func getPreallocatedUIDRange(ns *corev1.Namespace) (*int64, *int64, error) {
	annotationVal, ok := ns.Annotations[securityv1.UIDRangeAnnotation]
	if !ok {
		return nil, nil, fmt.Errorf("unable to find annotation %s", securityv1.UIDRangeAnnotation)
	}
	if len(annotationVal) == 0 {
		return nil, nil, fmt.Errorf("found annotation %s but it was empty", securityv1.UIDRangeAnnotation)
	}
	uidBlock, err := ParseBlock(annotationVal)
	if err != nil {
		return nil, nil, err
	}

	min := int64(uidBlock.Start)
	max := int64(uidBlock.End)
	log.Debugf("got preallocated values for min: %d, max: %d for uid range in namespace %s", min, max, ns.Name)
	return &min, &max, nil
}

// getPreallocatedSupplementalGroups gets the annotated value from the namespace.
func getPreallocatedSupplementalGroups(ns *corev1.Namespace) ([]securityv1.IDRange, error) {
	groups, err := getSupplementalGroupsAnnotation(ns)
	if err != nil {
		return nil, err
	}
	log.Debugf("got preallocated value for groups: %s in namespace %s", groups, ns.Name)

	blocks, err := parseSupplementalGroupAnnotation(groups)
	if err != nil {
		return nil, err
	}

	idRanges := []securityv1.IDRange{}
	for _, block := range blocks {
		rng := securityv1.IDRange{
			Min: int64(block.Start),
			Max: int64(block.End),
		}
		idRanges = append(idRanges, rng)
	}
	return idRanges, nil
}

// getSupplementalGroupsAnnotation provides a backwards compatible way to get supplemental groups
// annotations from a namespace by looking for SupplementalGroupsAnnotation and falling back to
// UIDRangeAnnotation if it is not found.
func getSupplementalGroupsAnnotation(ns *corev1.Namespace) (string, error) {
	groups, ok := ns.Annotations[securityv1.SupplementalGroupsAnnotation]
	if !ok {
		log.Debugf("unable to find supplemental group annotation %s falling back to %s", securityv1.SupplementalGroupsAnnotation, securityv1.UIDRangeAnnotation)

		groups, ok = ns.Annotations[securityv1.UIDRangeAnnotation]
		if !ok {
			return "", fmt.Errorf("unable to find supplemental group or uid annotation for namespace %s", ns.Name)
		}
	}

	if len(groups) == 0 {
		return "", fmt.Errorf("unable to find groups using %s and %s annotations", securityv1.SupplementalGroupsAnnotation, securityv1.UIDRangeAnnotation)
	}
	return groups, nil
}

// parseSupplementalGroupAnnotation parses the group annotation into blocks.
func parseSupplementalGroupAnnotation(groups string) ([]Block, error) {
	blocks := []Block{}
	segments := strings.Split(groups, ",")
	for _, segment := range segments {
		block, err := ParseBlock(segment)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no blocks parsed from annotation %s", groups)
	}
	return blocks, nil
}

// Functions below were copied from
// https://github.com/openshift/library-go/blob/561433066966536ac17f3c9852d7d85f7b7e1e36/pkg/security/uid/uid.go
// Copied here to avoid bringing tons of dependencies
// License is Apache 2.0: https://github.com/openshift/library-go/blob/561433066966536ac17f3c9852d7d85f7b7e1e36/LICENSE

type Block struct {
	Start uint32
	End   uint32
}

var (
	ErrBlockSlashBadFormat = fmt.Errorf("block not in the format \"<start>/<size>\"")
	ErrBlockDashBadFormat  = fmt.Errorf("block not in the format \"<start>-<end>\"")
)

func ParseBlock(in string) (Block, error) {
	if strings.Contains(in, "/") {
		var start, size uint32
		n, err := fmt.Sscanf(in, "%d/%d", &start, &size)
		if err != nil {
			return Block{}, err
		}
		if n != 2 {
			return Block{}, ErrBlockSlashBadFormat
		}
		return Block{Start: start, End: start + size - 1}, nil
	}

	var start, end uint32
	n, err := fmt.Sscanf(in, "%d-%d", &start, &end)
	if err != nil {
		return Block{}, err
	}
	if n != 2 {
		return Block{}, ErrBlockDashBadFormat
	}
	return Block{Start: start, End: end}, nil
}

func (b Block) String() string {
	return fmt.Sprintf("%d/%d", b.Start, b.Size())
}

func (b Block) RangeString() string {
	return fmt.Sprintf("%d-%d", b.Start, b.End)
}

func (b Block) Size() uint32 {
	return b.End - b.Start + 1
}
