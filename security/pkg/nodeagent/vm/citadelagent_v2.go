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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
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

type citadelAgent struct {
	// Configuration specific to Node Agent
	config   *Config
	pc       platform.Client
	caClient caClientInterface.Client
	identity string
	certUtil util.CertUtil
}

// Google Compute Engine specific consts/types.
// Later on if we support other types of VMs, we may need to move this to another package/file.
type googleJwtPayLoad struct {
	Aud    string `json:"aud"`
	Google map[string]map[string]interface{}
}

const (
	serviceAccountSuffix = ".iam.gserviceaccount.com"
	fsaPrefix            = "csm-fsa"
	// Fields from JWT from GCE metadata server.
	computeEngineField = "compute_engine"
	projectIDField     = "project_id"
)

// Start starts the node Agent.
// TODO(pitlv2109: Needs refactoring.
func (na *citadelAgent) Start() error {
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

func (na *citadelAgent) sendCSRUsingCANewProtocol() ([]byte, []string, error) {
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
	projectID, err := getProjectID(string(jwt))
	if err != nil {
		return nil, nil, fmt.Errorf("could not get project id wit error %v", err)
	}
	token, _, _ := stsClient.ExchangeToken(context.Background(), fmt.Sprintf("%s@%s%s", fsaPrefix, projectID, serviceAccountSuffix), string(jwt))
	certChainPEM, err := na.caClient.CSRSign(context.Background(), csrPEM, token, int64(na.config.CAClientConfig.RequestedCertTTL.Minutes()))
	if err != nil {
		return nil, nil, fmt.Errorf("error getting key and cert for %q: %v", na.identity, err)
	}
	return keyPEM, certChainPEM, nil
}

// getProjectID returns the project id from the provided jwt or an error.
func getProjectID(jwt string) (string, error) {
	jwtSplit := strings.Split(jwt, ".")
	if len(jwtSplit) != 3 {
		return "", fmt.Errorf("jwt may be invalid %s", jwt)
	}
	payload := jwtSplit[1]
	payloadBytes, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", err
	}
	structuredPayload := &googleJwtPayLoad{}
	err = json.Unmarshal(payloadBytes, &structuredPayload)
	if err != nil {
		return "", err
	}
	fieldNotFoundError := "jwt does not have %s field"
	if _, found := structuredPayload.Google[computeEngineField]; !found {
		return "", fmt.Errorf(fieldNotFoundError, computeEngineField)
	}
	projectID, found := structuredPayload.Google[computeEngineField][projectIDField]
	if !found {
		return "", fmt.Errorf(fieldNotFoundError, projectIDField)
	}
	return projectID.(string), nil
}
