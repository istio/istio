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

package mock

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	authorizationapi "k8s.io/api/authorization/v1"
	authorizationv1 "k8s.io/client-go/kubernetes/typed/authorization/v1"
)

var _ authorizationv1.SelfSubjectAccessReviewInterface = &SelfSubjectAccessReviewImpl{}

// SelfSubjectAccessReviewImpl is a mock implementation of SelfSubjectAccessReviewInterface
// Exported so that helpers can be used to set expected mock behavior
type SelfSubjectAccessReviewImpl struct {
	disallowed map[disallowKey]bool
}

// The subset of SelfSubjectAccessReview.Spec.ResourceAttributes we want to match on for controlling mock allow/disallow behavior
type disallowKey struct {
	verb     string
	group    string
	resource string
}

func newSelfSubjectAccessReviewInterface() authorizationv1.SelfSubjectAccessReviewInterface {
	return &SelfSubjectAccessReviewImpl{
		disallowed: make(map[disallowKey]bool),
	}
}

// Create implements authorizationv1.SelfSubjectAccessReviewInterface
func (i *SelfSubjectAccessReviewImpl) Create(ctx context.Context, sar *authorizationapi.SelfSubjectAccessReview,
	opts metav1.CreateOptions) (result *authorizationapi.SelfSubjectAccessReview, err error) {
	allowed := true
	if i.disallowed[newDisallowKey(sar.Spec.ResourceAttributes)] {
		allowed = false
	}

	result = sar.DeepCopy()
	result.Status = authorizationapi.SubjectAccessReviewStatus{Allowed: allowed}
	return result, nil
}

// CreateContext implements authorizationv1.SelfSubjectAccessReviewInterface
func (i *SelfSubjectAccessReviewImpl) CreateContext(ctx context.Context,
	sar *authorizationapi.SelfSubjectAccessReview) (result *authorizationapi.SelfSubjectAccessReview, err error) {

	panic("not implemented")
}

// DisallowResourceAttributes is a helper for testing that marks particular resource attributes as not allowed in the mock.
func (i *SelfSubjectAccessReviewImpl) DisallowResourceAttributes(r *authorizationapi.ResourceAttributes) {
	i.disallowed[newDisallowKey(r)] = true
}

func newDisallowKey(r *authorizationapi.ResourceAttributes) disallowKey {
	return disallowKey{
		verb:     r.Verb,
		group:    r.Group,
		resource: r.Resource,
	}
}
