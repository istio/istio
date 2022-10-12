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

package tokenreview

import (
	"context"
	"fmt"
	"strings"

	k8sauth "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/security"
)

// nolint: lll
// From https://github.com/kubernetes/kubernetes/blob/4f2faa2f1ce8f49983173ef29214156afdf405f9/staging/src/k8s.io/apiserver/pkg/authentication/serviceaccount/util.go#L41
const (
	// PodNameKey is the key used in a user's "extra" to specify the pod name of
	// the authenticating request.
	PodNameKey = "authentication.kubernetes.io/pod-name"
	// PodUIDKey is the key used in a user's "extra" to specify the pod UID of
	// the authenticating request.
	PodUIDKey = "authentication.kubernetes.io/pod-uid"
)

// ValidateK8sJwt validates a k8s JWT at API server.
// Return {<namespace>, <serviceaccountname>} in the targetToken when the validation passes.
// Otherwise, return the error.
// targetToken: the JWT of the K8s service account to be reviewed
// aud: list of audiences to check. If empty 1st party tokens will be checked.
func ValidateK8sJwt(kubeClient kubernetes.Interface, targetToken string, aud []string) (security.KubernetesInfo, error) {
	tokenReview := &k8sauth.TokenReview{
		Spec: k8sauth.TokenReviewSpec{
			Token: targetToken,
		},
	}
	if aud != nil {
		tokenReview.Spec.Audiences = aud
	}
	reviewRes, err := kubeClient.AuthenticationV1().TokenReviews().Create(context.TODO(), tokenReview, metav1.CreateOptions{})
	if err != nil {
		return security.KubernetesInfo{}, err
	}

	return getTokenReviewResult(reviewRes)
}

func getTokenReviewResult(tokenReview *k8sauth.TokenReview) (security.KubernetesInfo, error) {
	if tokenReview.Status.Error != "" {
		return security.KubernetesInfo{}, fmt.Errorf("the service account authentication returns an error: %v",
			tokenReview.Status.Error)
	}
	// An example SA token:
	// {"alg":"RS256","typ":"JWT"}
	// {"iss":"kubernetes/serviceaccount",
	//  "kubernetes.io/serviceaccount/namespace":"default",
	//  "kubernetes.io/serviceaccount/secret.name":"example-pod-sa-token-h4jqx",
	//  "kubernetes.io/serviceaccount/service-account.name":"example-pod-sa",
	//  "kubernetes.io/serviceaccount/service-account.uid":"ff578a9e-65d3-11e8-aad2-42010a8a001d",
	//  "sub":"system:serviceaccount:default:example-pod-sa"
	//  }

	// An example token review status
	// "status":{
	//   "authenticated":true,
	//   "user":{
	//     "username":"system:serviceaccount:default:example-pod-sa",
	//     "uid":"ff578a9e-65d3-11e8-aad2-42010a8a001d",
	//     "groups":["system:serviceaccounts","system:serviceaccounts:default","system:authenticated"]
	//    }
	// }

	if !tokenReview.Status.Authenticated {
		return security.KubernetesInfo{}, fmt.Errorf("the token is not authenticated")
	}
	inServiceAccountGroup := false
	for _, group := range tokenReview.Status.User.Groups {
		if group == "system:serviceaccounts" {
			inServiceAccountGroup = true
			break
		}
	}
	if !inServiceAccountGroup {
		return security.KubernetesInfo{}, fmt.Errorf("the token is not a service account")
	}
	// "username" is in the form of system:serviceaccount:{namespace}:{service account name}",
	// e.g., "username":"system:serviceaccount:default:example-pod-sa"
	subStrings := strings.Split(tokenReview.Status.User.Username, ":")
	if len(subStrings) != 4 {
		return security.KubernetesInfo{}, fmt.Errorf("invalid username field in the token review result")
	}

	return security.KubernetesInfo{
		PodName:           extractExtra(tokenReview, PodNameKey),
		PodNamespace:      subStrings[2],
		PodUID:            extractExtra(tokenReview, PodUIDKey),
		PodServiceAccount: subStrings[3],
	}, nil
}

func extractExtra(review *k8sauth.TokenReview, s string) string {
	values, ok := review.Status.User.Extra[s]
	if !ok || len(values) == 0 {
		return ""
	}
	return values[0]
}
