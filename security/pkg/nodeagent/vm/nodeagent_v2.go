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

package vm

import (
	"context"
	"fmt"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/caclient"
	caClientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"
	"istio.io/istio/security/pkg/nodeagent/plugin/providers/google/stsclient"
	pkiutil "istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/platform"
	"istio.io/istio/security/pkg/util"
)

// This file is similar to nodeagent.go, which implements NodeAgent interface.
// However, this will implement NodeAgent using the new CA protocol which can be found here
// https://github.com/istio/istio/blob/master/security/proto/istioca.proto

type nodeAgentInternalV2 struct {
	// Configuration specific to Node Agent
	config   *Config
	pc       platform.Client
	caClient caClientInterface.Client
	identity string
	certUtil util.CertUtil
}

// Start starts the node Agent.
func (na *nodeAgentInternalV2) Start() error {
	if na.config == nil {
		return fmt.Errorf("node Agent configuration is nil")
	}

	if !na.pc.IsProperPlatform() {
		return fmt.Errorf("node Agent is not running on the right platform")
	}

	log.Infof("Node Agent V2 starts successfully.")

	retries := 0
	retrialInterval := na.config.CAClientConfig.CSRInitialRetrialInterval
	identity, err := na.pc.GetServiceIdentity()
	if err != nil {
		return err
	}
	na.identity = identity
	var success bool
	for {
		log.Infof("Sending CSR (retrial #%d) ...", retries)
		keyPEM, certChainPEM, csrError := na.sendCSRUsingCANewProtocol()

		if csrError != nil {
			log.Errorf("%v", csrError)
			success = false
		} else {
			waitTime, ttlErr := na.certUtil.GetWaitTime([]byte(certChainPEM[0]), time.Now())
			if ttlErr != nil {
				log.Errorf("Error getting TTL from approved cert: %v", ttlErr)
				success = false
			} else {
				var certChain []byte
				for _, c := range certChainPEM {
					certChain = append(certChain, []byte(c)...)
				}
				if err = caclient.SaveKeyCert(na.config.CAClientConfig.KeyFile,
					na.config.CAClientConfig.CertChainFile,
					keyPEM, certChain); err != nil {
					return err
				}
				log.Infof("CSR is approved successfully. Will renew cert in %s", waitTime.String())
				fmt.Println(certChainPEM)
				retries = 0
				retrialInterval = na.config.CAClientConfig.CSRInitialRetrialInterval
				timer := time.NewTimer(waitTime)
				<-timer.C
				success = true
			}
		}

		if !success {
			if retries >= na.config.CAClientConfig.CSRMaxRetries {
				return fmt.Errorf(
					"node agent can't get the CSR approved from Istio CA after max number of retries (%d)",
					na.config.CAClientConfig.CSRMaxRetries)
			}
			retries++
			timer := time.NewTimer(retrialInterval)
			// Exponentially increase the backoff time.
			retrialInterval *= 2
			<-timer.C
		}
	}
}

func (na *nodeAgentInternalV2) sendCSRUsingCANewProtocol() ([]byte, []string, error) {
	options := pkiutil.CertOptions{
		Host:       na.identity,
		Org:        na.config.CAClientConfig.Org,
		RSAKeySize: na.config.CAClientConfig.RSAKeySize,
		IsDualUse:  na.config.DualUse,
	}

	csrPEM, keyPEM, err := pkiutil.GenCSR(options)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generated key cert for %q: %v", na.identity, err)
	}
	jwt, err := na.pc.GetAgentCredential()
	if err != nil {
		return nil, nil, fmt.Errorf("error getting JWT from metadata server for %q: %v", na.identity, err)
	}
	stsClient := stsclient.NewPlugin()
	// TODO(pitlv2109): Periodically update the JWT and OAuth2 token.
	// TODO(pitlv2109): Extract the project_id field from the JWT.
	token, _, _ := stsClient.ExchangeToken(context.Background(), "csm-fsa@mesh-expansion-235023.iam.gserviceaccount.com", string(jwt))
	certChainPEM, err := na.caClient.CSRSign(context.Background(), csrPEM, token, int64(na.config.CAClientConfig.RequestedCertTTL.Minutes()))
	if err != nil {
		return nil, nil, fmt.Errorf("error getting key and cert for %q: %v", na.identity, err)
	}
	return keyPEM, certChainPEM, nil
}
