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

package xds

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

type istioctlTokenSupplier struct {
	// The token itself
	Token   string
	Expires time.Time

	// Check and generate tokens
	opts       *clioptions.CentralControlPlaneOptions
	kubeClient kube.ExtendedClient

	// Send messages to user on this channel, typically cmd.OutOrStderr()
	userWriter io.Writer
}

var _ credentials.PerRPCCredentials = &istioctlTokenSupplier{}

const (
	// defaultExpirationSeconds is how long-lived a token to request (an hour)
	defaultExpirationSeconds = 60 * 60

	// sunsetPeriod is how long before expiration that we start trying to renew (a minute)
	sunsetPeriod = 60 * time.Second

	tokenServiceAccount = "default"
	tokenNamespace      = "default"
)

var (
	// scope is for dev logging.  Warning: log levels are not set by --log_output_level until command is Run().
	scope = log.RegisterScope("cli", "istioctl", 0)
)

// DialOptions constructs gRPC dial options from command line configuration
func DialOptions(opts *clioptions.CentralControlPlaneOptions, kubeClient kube.ExtendedClient, userWriter io.Writer) ([]grpc.DialOption, error) {
	// If we are using the insecure 15010 don't bother getting a token
	if opts.Plaintext || opts.CertDir != "" {
		return make([]grpc.DialOption, 0), nil
	}

	// Use bearer token
	supplier, err := newIstioctlTokenSupplier(opts, kubeClient, userWriter)
	if err != nil {
		return nil, err
	}
	return []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(
			&tls.Config{
				// Always skip verifying, because without it we always get "certificate signed by unknown authority".
				// We don't se the XDSSAN for the same reason.
				InsecureSkipVerify: true,
			})),
		grpc.WithPerRPCCredentials(supplier),
	}, err
}

func newIstioctlTokenSupplier(opts *clioptions.CentralControlPlaneOptions, kubeClient kube.ExtendedClient,
	userWriter io.Writer) (*istioctlTokenSupplier, error) {
	its := istioctlTokenSupplier{
		opts:       opts,
		kubeClient: kubeClient,
		userWriter: userWriter,
	}
	err := its.initialize()
	if err != nil {
		return nil, err
	}
	return &its, nil
}

func (its *istioctlTokenSupplier) initialize() error {
	err := its.getCachedToken()
	expired := time.Until(its.Expires) < sunsetPeriod
	if err == nil && !expired {
		// If it loaded, and looks like it hasn't expired, review it
		err = its.reviewToken(context.TODO())
	}
	if err != nil || expired {
		// If we failed to load, failed review, or expired, create a new token
		err = its.createServiceAccountToken(context.TODO())
		if err != nil {
			return err
		}
	}
	// Ignore errors saving the token.  we are initialized and can still use the token we have
	_ = its.saveToken()
	return nil
}

func (its *istioctlTokenSupplier) reviewToken(ctx context.Context) error {
	tr, err := its.kubeClient.Kube().AuthenticationV1().TokenReviews().Create(ctx, &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{
			Token:     its.Token,
			Audiences: []string{"istio-ca"},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		// This kind of error means we talked to the API Server.  It doesn't meet the token is valid.
		return err
	}
	if tr.Status.Error != "" {
		// This kind of error means something is wrong with the JWT itself
		fmt.Fprintf(its.userWriter, "Cached JWT token invalid; will refresh: %s\n", tr.Status.Error)
		return errors.New(tr.Status.Error)
	}
	return nil
}

func (its *istioctlTokenSupplier) createServiceAccountToken(ctx context.Context) error {
	expirationSeconds := int64(defaultExpirationSeconds)
	tokenRequest, err := its.kubeClient.Kube().CoreV1().ServiceAccounts(tokenNamespace).CreateToken(ctx, tokenServiceAccount,
		&authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				Audiences:         []string{"istio-ca"},
				ExpirationSeconds: &expirationSeconds,
			},
		}, metav1.CreateOptions{})

	if err != nil {
		return err
	}
	its.Token = tokenRequest.Status.Token
	its.Expires = tokenRequest.Status.ExpirationTimestamp.Time
	return nil
}

func (its *istioctlTokenSupplier) getCachedToken() error {
	by, err := ioutil.ReadFile(its.opts.TokenFile)
	if err != nil {
		return err
	}
	return json.Unmarshal(by, its)
}

func (its *istioctlTokenSupplier) saveToken() error {
	by, err := json.MarshalIndent(its, "", "  ")
	if err != nil {
		return err
	}
	err = os.MkdirAll(filepath.Dir(its.opts.TokenFile), 0700)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(its.opts.TokenFile, by, 0600)
	if err != nil {
		fmt.Fprintf(its.userWriter, "Could not cache token: %v\n", err)
	}
	return err
}

// GetRequestMetadata() fulfills the grpc/credentials.PerRPCCredentials interface
func (its *istioctlTokenSupplier) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	if time.Until(its.Expires) < sunsetPeriod {
		scope.Debug("GetRequestMetadata will generate a new token to replace one that is about to expire")
		// We have no 'renew' method, just request a new token
		err := its.createServiceAccountToken(context.TODO())
		if err == nil {
			// If we successfully renewed, save the token.  Ignore problems saving, the goal is to supply the header,
			// not to cache it.
			err = its.saveToken()
			if err != nil {
				scope.Infof("GetRequestMetadata failed to cache token: %v", err.Error())
			}
		} else {
			scope.Infof("GetRequestMetadata failed to recreate token: %v", err.Error())
		}
	}

	return map[string]string{
		"authorization": "Bearer " + its.Token,
	}, nil
}

// RequireTransportSecurity() fulfills the grpc/credentials.PerRPCCredentials interface
func (its *istioctlTokenSupplier) RequireTransportSecurity() bool {
	return false
}
