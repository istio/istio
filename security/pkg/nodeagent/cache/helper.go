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

package cache

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/rand"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gogo/status"
	"google.golang.org/grpc/codes"

	"istio.io/istio/security/pkg/nodeagent/model"
)

// SecretExist checks if secret already existed.
// This API is used for sds server to check if coming request is ack request.
func (sc *SecretCache) SecretExist(connectionID, resourceName, token, version string) bool {
	key := ConnKey{
		ConnectionID: connectionID,
		ResourceName: resourceName,
	}
	val, exist := sc.secrets.Load(key)
	if !exist {
		return false
	}

	e := val.(model.SecretItem)
	return e.ResourceName == resourceName && e.Token == token && e.Version == version
}

func (sc *SecretCache) callbackWithTimeout(connectionID string, secretName string, secret *model.SecretItem) {
	c := make(chan struct{})
	conIDresourceNamePrefix := cacheLogPrefix(connectionID, secretName)
	go func() {
		defer close(c)
		if sc.notifyCallback != nil {
			if err := sc.notifyCallback(connectionID, secretName, secret); err != nil {
				cacheLog.Errorf("%s failed to notify secret change for proxy: %v",
					conIDresourceNamePrefix, err)
			}
		} else {
			cacheLog.Warnf("%s secret cache notify callback isn't set", conIDresourceNamePrefix)
		}
	}()
	select {
	case <-c:
		return // completed normally
	case <-time.After(notifyK8sSecretTimeout):
		cacheLog.Warnf("%s notify secret change for proxy got timeout", conIDresourceNamePrefix)
	}
}

func (sc *SecretCache) keyCertRotationJob() {
	// Wake up once in a while and refresh stale items.
	sc.rotationTicker = time.NewTicker(sc.configOptions.RotationInterval)
	for {
		select {
		case <-sc.rotationTicker.C:
			sc.rotate(false /*updateRootFlag*/)
		case <-sc.closing:
			if sc.rotationTicker != nil {
				sc.rotationTicker.Stop()
			}
		}
	}
}

// IsIngressGatewaySecretReady returns true if node agent is working in ingress gateway agent mode
// and needs to wait for ingress gateway secret to be ready.
func (sc *SecretCache) ShouldWaitForIngressGatewaySecret(connectionID, resourceName, token string) bool {
	// If node agent works as workload agent, node agent does not expect any ingress gateway secret.
	if sc.fetcher.UseCaClient {
		return false
	}

	key := ConnKey{
		ConnectionID: connectionID,
		ResourceName: resourceName,
	}
	// Add an entry into cache, so that when ingress gateway secret is ready, gateway agent is able to
	// notify the ingress gateway and push the secret to via connect ID.
	if _, found := sc.secrets.Load(key); !found {
		t := time.Now()
		dummySecret := &model.SecretItem{
			ResourceName: resourceName,
			Token:        token,
			CreatedTime:  t,
			Version:      t.String(),
		}
		sc.secrets.Store(key, *dummySecret)
	}

	conIDresourceNamePrefix := cacheLogPrefix(connectionID, resourceName)
	// If node agent works as ingress gateway agent, searches for kubernetes secret and verify secret
	// is not empty.
	cacheLog.Debugf("%s calling SecretFetcher to search for secret %s",
		conIDresourceNamePrefix, resourceName)
	_, exist := sc.fetcher.FindIngressGatewaySecret(resourceName)
	// If kubernetes secret does not exist, need to wait for secret.
	if !exist {
		cacheLog.Warnf("%s SecretFetcher cannot find secret %s from cache",
			conIDresourceNamePrefix, resourceName)
		return true
	}

	return false
}

// parseCertAndGetExpiryTimestamp parses certificate and returns cert expire time, or return error
// if fails to parse certificate.
func parseCertAndGetExpiryTimestamp(certByte []byte) (time.Time, error) {
	block, _ := pem.Decode(certByte)
	if block == nil {
		cacheLog.Errorf("Failed to decode certificate")
		return time.Time{}, fmt.Errorf("failed to decode certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		cacheLog.Errorf("Failed to parse certificate: %v", err)
		return time.Time{}, fmt.Errorf("failed to parse certificate: %v", err)
	}
	return cert.NotAfter, nil
}

func (sc *SecretCache) shouldRefresh(s *model.SecretItem) bool {
	// secret should be refreshed before it expired, SecretRefreshGraceDuration is the grace period;
	return time.Now().After(s.ExpireTime.Add(-sc.configOptions.SecretRefreshGraceDuration))
}

func (sc *SecretCache) isTokenExpired() bool {
	// skip check if the token passed from envoy is always valid (ex, normal k8s sa JWT).
	if sc.configOptions.AlwaysValidTokenFlag {
		return false
	}

	if atomic.LoadUint32(&sc.skipTokenExpireCheck) == 1 {
		return true
	}
	// TODO(quanlin), check if token has expired.
	return false
}

func constructCSRHostName(trustDomain, token string) (string, error) {
	// If token is jwt format, construct host name from jwt with format like spiffe://cluster.local/ns/foo/sa/sleep,
	strs := strings.Split(token, ".")
	if len(strs) != 3 {
		return "", fmt.Errorf("invalid k8s jwt token")
	}

	payload := strs[1]
	if l := len(payload) % 4; l > 0 {
		payload += strings.Repeat("=", 4-l)
	}
	dp, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("invalid k8s jwt token: %v", err)
	}

	var jp k8sJwtPayload
	if err = json.Unmarshal(dp, &jp); err != nil {
		return "", fmt.Errorf("invalid k8s jwt token: %v", err)
	}

	// sub field in jwt should be in format like: system:serviceaccount:foo:bar
	ss := strings.Split(jp.Sub, ":")
	if len(ss) != 4 {
		return "", fmt.Errorf("invalid sub field in k8s jwt token")
	}
	ns := ss[2] //namespace
	sa := ss[3] //service account

	domain := "cluster.local"
	if trustDomain != "" {
		domain = trustDomain
	}

	return fmt.Sprintf(identityTemplate, domain, ns, sa), nil
}

func isRetryableErr(c codes.Code, httpRespCode int, isGrpc bool) bool {
	if isGrpc {
		switch c {
		case codes.Canceled, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted, codes.Internal, codes.Unavailable:
			return true
		}
	} else {
		if httpRespCode >= 500 && !(httpRespCode == 501 || httpRespCode == 505 || httpRespCode == 511) {
			return true
		}
	}
	return false
}

// cacheLogPrefix returns a unified log prefix.
func cacheLogPrefix(conID, resourceName string) string {
	lPrefix := fmt.Sprintf("CONNECTION ID: %s, RESOURCE NAME: %s, EVENT:", conID, resourceName)
	return lPrefix
}

// sendRetriableRequest sends retriable requests for either CSR or ExchangeToken.
// Prior to sending the request, it also sleep random millisecond to avoid thundering herd problem.
func (sc *SecretCache) sendRetriableRequest(ctx context.Context, csrPEM []byte, providedExchangedToken, resourceName string, isCSR bool) ([]string, error) {
	backOffInMilliSec := rand.Int63n(sc.configOptions.InitialBackoff)
	cacheLog.Debugf("Wait for %d millisec for initial CSR", backOffInMilliSec)
	// Add a jitter to initial CSR to avoid thundering herd problem.
	time.Sleep(time.Duration(backOffInMilliSec) * time.Millisecond)

	startTime := time.Now()
	var retry int64
	var certChainPEM []string
	exchangedToken := providedExchangedToken
	var requestErrorString string
	var err error

	// Keep trying until no error or timeout.
	for {
		var httpRespCode int
		if isCSR {
			requestErrorString = fmt.Sprintf("CSR for %q", resourceName)
			certChainPEM, err = sc.fetcher.CaClient.CSRSign(
				ctx, csrPEM, exchangedToken, int64(sc.configOptions.SecretTTL.Seconds()))
		} else {
			requestErrorString = "Token exchange"
			p := sc.configOptions.Plugins[0]
			exchangedToken, _, httpRespCode, err = p.ExchangeToken(ctx, sc.configOptions.TrustDomain, exchangedToken)
		}

		if err == nil {
			break
		}

		// If non-retryable error, fail the request by returning err
		if !isRetryableErr(status.Code(err), httpRespCode, isCSR) {
			cacheLog.Errorf("%s hit non-retryable error %v", requestErrorString, err)
			return nil, err
		}

		// If reach envoy timeout, fail the request by returning err
		if startTime.Add(time.Millisecond * envoyDefaultTimeoutInMilliSec).Before(time.Now()) {
			cacheLog.Errorf("%s retry timed out %v", requestErrorString, err)
			return nil, err
		}

		retry++
		backOffInMilliSec = rand.Int63n(retry * initialBackOffIntervalInMilliSec)
		time.Sleep(time.Duration(backOffInMilliSec) * time.Millisecond)
		cacheLog.Warnf("%s failed with error: %v, retry in %d millisec", requestErrorString, err, backOffInMilliSec)
	}

	if isCSR {
		return certChainPEM, nil
	}
	return []string{exchangedToken}, nil
}

// getExchangedToken gets the exchanged token for the CSR. The token is either the k8s jwt token of the
// workload or another token from a plug in provider.
func (sc *SecretCache) getExchangedToken(ctx context.Context, k8sJwtToken string) (string, error) {
	exchangedTokens := []string{k8sJwtToken}
	var err error
	if sc.configOptions.Plugins != nil && len(sc.configOptions.Plugins) > 0 {
		// Currently the only plugin is GoogleTokenExchange, so we only extract the first possible plugin.
		if len(sc.configOptions.Plugins) > 1 {
			cacheLog.Error("found more than one plugin")
			return "", err
		}
		exchangedTokens, err = sc.sendRetriableRequest(ctx, nil, k8sJwtToken, "", false)
		if err != nil || len(exchangedTokens) == 0 {
			cacheLog.Errorf("failed to exchange token: %v", err)
			return "", err
		}
	}
	return exchangedTokens[0], nil
}
