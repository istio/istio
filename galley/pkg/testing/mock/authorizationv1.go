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
	authorizationv1 "k8s.io/client-go/kubernetes/typed/authorization/v1"
	"k8s.io/client-go/rest"
)

type authorizationv1Impl struct {
	selfSubjectAccessReviews authorizationv1.SelfSubjectAccessReviewInterface
}

var _ authorizationv1.AuthorizationV1Interface = &authorizationv1Impl{}

func (a *authorizationv1Impl) RESTClient() rest.Interface {
	panic("not implemented")
}

func (a *authorizationv1Impl) LocalSubjectAccessReviews(namespace string) authorizationv1.LocalSubjectAccessReviewInterface {
	panic("not implemented")
}

func (a *authorizationv1Impl) SelfSubjectAccessReviews() authorizationv1.SelfSubjectAccessReviewInterface {
	return a.selfSubjectAccessReviews
}

func (a *authorizationv1Impl) SelfSubjectRulesReviews() authorizationv1.SelfSubjectRulesReviewInterface {
	panic("not implemented")
}

func (a *authorizationv1Impl) SubjectAccessReviews() authorizationv1.SubjectAccessReviewInterface {
	panic("not implemented")
}
