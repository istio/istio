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

package caclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	caClientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

var (
	keyFactorCAClientLog    = log.RegisterScope("keyfactor", "KeyFactor CA client debugging", 0)
	certificateAuthorityVar = env.RegisterStringVar("KEYFACTOR_CA", "", "Name of certificate").Get()
	authTokenVar            = env.RegisterStringVar("KEYFACTOR_AUTH_TOKEN", "", "Auth token of keyfactor").Get()
	enrollCSRPathVar        = env.RegisterStringVar("KEYFACTOR_ENROLL_PATH", "/KeyfactorAPI/Enrollment/CSR", "API path of enroll certificate").Get()
	certificateTemplateVar  = env.RegisterStringVar("KEYFACTOR_CA_TEMPLATE", "Istio", "Keyfactor certificate template").Get()
	appKeyVar               = env.RegisterStringVar("KEYFACTOR_APPKEY", "", "KeyFactor Api Key").Get()

	instanceIPVar   = env.RegisterStringVar("ISTIO_META_INSTANCE_IP", "", "").Get()
	podNameVar      = env.RegisterStringVar("ISTIO_META_POD_NAME", "", "").Get()
	podNamespaceVar = env.RegisterStringVar("ISTIO_META_POD_NAMESPACE", "", "").Get()
	clusterIDVar    = env.RegisterStringVar("ISTIO_META_CLUSTER_ID", "", "").Get()
)

type keyFactorCAClient struct {
	caEndpoint   string
	enableTLS    bool
	client       *http.Client
	trustDomain  string
	clusterID    string
	podName      string
	podNamespace string
	instanceIP   string
}

type san struct {
	IP4 []string `json:"ip4"`
	DNS []string `json:"dns"`
}

type metadata struct {
	Cluster      string `json:"Cluster"`
	Service      string `json:"Service"`
	PodNamespace string `json:"PodNamespace"`
	TrustDomain  string `json:"TrustDomain"`
}

type keyfactorRequestPayload struct {
	CSR                  string   `json:"CSR"`
	CertificateAuthority string   `json:"CertificateAuthority"`
	IncludeChain         bool     `json:"IncludeChain"`
	TimeStamp            string   `json:"TimeStamp"`
	Template             string   `json:"Template"`
	SANs                 san      `json:"SANs"`
	Metadata             metadata `json:"Metadata"`
}

// KeyfactorResponse response structure for keyfactor server
type keyfactorResponse struct {
	CertificateInformation struct {
		SerialNumber       string      `json:"SerialNumber"`
		IssuerDN           string      `json:"IssuerDN"`
		Thumbprint         string      `json:"Thumbprint"`
		KeyfactorID        int         `json:"KeyfactorID"`
		KeyfactorRequestID int         `json:"KeyfactorRequestId"`
		Certificates       []string    `json:"Certificates"`
		RequestDisposition string      `json:"RequestDisposition"`
		DispositionMessage string      `json:"DispositionMessage"`
		EnrollmentContext  interface{} `json:"EnrollmentContext"`
	} `json:"CertificateInformation"`
}

// NewKeyFactorCAClient create a CA client for KeyFactor CA.
func NewKeyFactorCAClient(endpoint string, tls bool, trustDomain string, rootCert []byte) (caClientInterface.Client, error) {
	c := &keyFactorCAClient{
		caEndpoint:   endpoint,
		enableTLS:    tls,
		podName:      podNameVar,
		trustDomain:  trustDomain,
		clusterID:    clusterIDVar,
		podNamespace: podNamespaceVar,
		instanceIP:   net.ParseIP(instanceIPVar).String(),
	}

	if !tls {
		c.client = &http.Client{
			Timeout: time.Second * 10,
		}
	} else {
		c.client = &http.Client{
			Timeout: time.Second * 10,
		}
	}

	return c, nil
}

// CSRSign calls KeyFactor CA to sign a CSR.
func (cl *keyFactorCAClient) CSRSign(ctx context.Context, reqID string, csrPEM []byte, subjectID string,
	certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error) {

	bytesRepresentation, err := json.Marshal(keyfactorRequestPayload{
		CSR:                  string(csrPEM),
		CertificateAuthority: certificateAuthorityVar,
		IncludeChain:         true,
		Template:             certificateTemplateVar,
		TimeStamp:            time.Now().Format(time.RFC3339),
		SANs: san{
			DNS: []string{cl.trustDomain},
			IP4: []string{cl.instanceIP},
		},
		Metadata: metadata{
			Cluster:      cl.clusterID,
			Service:      cl.podName,
			PodNamespace: cl.podNamespace,
		},
	})

	requestCSR, err := http.NewRequest("POST", cl.caEndpoint+enrollCSRPathVar, bytes.NewBuffer(bytesRepresentation))

	if err != nil {
		return nil, fmt.Errorf("Cannot create request with url: %v", cl.caEndpoint+enrollCSRPathVar)
	}

	requestCSR.Header.Set("authorization", authTokenVar)
	requestCSR.Header.Set("x-keyfactor-requested-with", "APIClient")
	requestCSR.Header.Set("x-Keyfactor-appKey", appKeyVar)
	requestCSR.Header.Set("x-certificateformat", "PEM")
	requestCSR.Header.Set("Content-Type", "application/json")

	if err != nil {
		keyFactorCAClientLog.Errorf("Request to keyfactor is invalid: %v", err)
		return nil, fmt.Errorf("Request to keyfactor is invalid: %v", err)
	}

	res, err := cl.client.Do(requestCSR)
	if err != nil {
		return nil, fmt.Errorf("Could not request to KeyfactorCA server: %v", err)
	}
	defer res.Body.Close()
	status := res.StatusCode

	if status == http.StatusOK {
		jsonResponse := &keyfactorResponse{}
		json.NewDecoder(res.Body).Decode(&jsonResponse)
	}

	var errorMessage interface{}
	json.NewDecoder(res.Body).Decode(&errorMessage)
	keyFactorCAClientLog.Errorf("Request failed with status: %v, message: %v", status, errorMessage)
	return nil, fmt.Errorf("Request failed with status: %v, message: %v", status, errorMessage)
}

func getCertFromResponse(jsonResponse keyfactorResponse) ([]string, error) {
	certChains := []string{}

	template := "-----BEGIN CERTIFICATE-----\n%s\n-----END CERTIFICATE-----\n"

	for _, i := range jsonResponse.CertificateInformation.Certificates {
		certChains = append(certChains, fmt.Sprintf(template, i))
	}
	return certChains, nil
}
