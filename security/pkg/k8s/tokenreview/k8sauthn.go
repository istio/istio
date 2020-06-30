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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	k8sauth "k8s.io/api/authentication/v1"

	"istio.io/istio/pkg/jwt"
)

const (
	// The default audience for SDS trustworthy JWT. This is to make sure that the CSR requests
	// contain the JWTs intended for Citadel.
	DefaultAudience = "istio-ca"
)

// ValidateK8sJwt validates a k8s JWT at API server.
// Return {<namespace>, <serviceaccountname>} in the targetToken when the validation passes.
// Otherwise, return the error.
// targetToken: the JWT of the K8s service account to be reviewed
// jwtPolicy: the policy for validating JWT.
func ValidateK8sJwt(kubeClient kubernetes.Interface, targetToken, jwtPolicy string) ([]string, error) {
	tokenReview := &k8sauth.TokenReview{
		Spec: k8sauth.TokenReviewSpec{
			Token: targetToken,
		},
	}
	if jwtPolicy == jwt.PolicyThirdParty {
		tokenReview.Spec.Audiences = []string{DefaultAudience}
	} else if jwtPolicy != jwt.PolicyFirstParty {
		return nil, fmt.Errorf("invalid JWT policy: %v", jwtPolicy)
	}
	reviewRes, err := kubeClient.AuthenticationV1().TokenReviews().Create(context.TODO(), tokenReview, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	return getTokenReviewResult(reviewRes)
}

// TODO: add test case
func getTokenReviewResult(tokenReview *k8sauth.TokenReview) ([]string, error) {
	if tokenReview.Status.Error != "" {
		return nil, fmt.Errorf("the service account authentication returns an error: %v",
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
		return nil, fmt.Errorf("the token is not authenticated")
	}
	inServiceAccountGroup := false
	for _, group := range tokenReview.Status.User.Groups {
		if group == "system:serviceaccounts" {
			inServiceAccountGroup = true
			break
		}
	}
	if !inServiceAccountGroup {
		return nil, fmt.Errorf("the token is not a service account")
	}
	// "username" is in the form of system:serviceaccount:{namespace}:{service account name}",
	// e.g., "username":"system:serviceaccount:default:example-pod-sa"
	subStrings := strings.Split(tokenReview.Status.User.Username, ":")
	if len(subStrings) != 4 {
		return nil, fmt.Errorf("invalid username field in the token review result")
	}
	namespace := subStrings[2]
	saName := subStrings[3]
	return []string{namespace, saName}, nil
}
