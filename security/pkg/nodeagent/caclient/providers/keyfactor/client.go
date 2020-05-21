// Copyright 2020 Istio Authors
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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	caClientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"
	"istio.io/pkg/log"
)

var (
	keyFactorCAClientLog = log.RegisterScope("keyfactor", "KeyFactor CA client debugging", 0)
)

// KeyfactorCAClientMetadata struct to carry metadata of Keyfactor Client
type KeyfactorCAClientMetadata struct {
	TrustDomain  string
	ClusterID    string
	PodNamespace string
	PodName      string
	PodIP        string
}

// KeyFactorCAClient struct to define http client for KeyfactorCA
type KeyFactorCAClient struct {
	CaEndpoint      string
	EnableTLS       bool
	Client          *http.Client
	CustomMetadatas map[string]string
	Metadata        *KeyfactorCAClientMetadata
	ClientOptions   *KeyfactorConfig
}

type san struct {
	IP4 []string `json:"ip4"`
	DNS []string `json:"dns"`
}

type keyfactorRequestPayload struct {
	CSR                  string            `json:"CSR"`
	CertificateAuthority string            `json:"CertificateAuthority"`
	IncludeChain         bool              `json:"IncludeChain"`
	TimeStamp            string            `json:"TimeStamp"`
	Template             string            `json:"Template"`
	SANs                 san               `json:"SANs"`
	Metadata             map[string]string `json:"Metadata"`
}

// CertificateInformation response structure for keyfactor server
type CertificateInformation struct {
	SerialNumber       string      `json:"SerialNumber"`
	IssuerDN           string      `json:"IssuerDN"`
	Thumbprint         string      `json:"Thumbprint"`
	KeyfactorID        int         `json:"KeyfactorID"`
	KeyfactorRequestID int         `json:"KeyfactorRequestId"`
	Certificates       []string    `json:"Certificates"`
	RequestDisposition string      `json:"RequestDisposition"`
	DispositionMessage string      `json:"DispositionMessage"`
	EnrollmentContext  interface{} `json:"EnrollmentContext"`
}

// KeyfactorResponse response structure for keyfactor server
type KeyfactorResponse struct {
	CertificateInformation CertificateInformation `json:"CertificateInformation"`
}

// NewKeyFactorCAClient create a CA client for KeyFactor CA.
func NewKeyFactorCAClient(endpoint string, enableTLS bool, rootCert []byte, metadata *KeyfactorCAClientMetadata) (caClientInterface.Client, error) {

	keyfactorConfig, err := LoadKeyfactorConfigFromENV()

	if err != nil {
		return nil, fmt.Errorf("Cannot load keyfactor config: %v", err)
	}

	keyFactorCAClientLog.Infof("Create keyfactor metadatas: %v", keyfactorConfig.CustomMetadatas)

	customMetadatas := make(map[string]string)

	for _, field := range keyfactorConfig.CustomMetadatas {
		switch field.Name {
		case "Cluster":
			customMetadatas[field.Alias] = metadata.ClusterID

		case "Service":
			serviceName := metadata.PodName
			if splitPodName := strings.Split(metadata.PodName, "-"); len(splitPodName) > 2 {
				// example: service-name-A-v1-roiwe0239-24jfef9 => service-name-A-v1
				arrayOfServiceNames := splitPodName[0 : len(splitPodName)-2]
				serviceName = strings.Join(arrayOfServiceNames, "-")
			}
			customMetadatas[field.Alias] = serviceName
		case "PodName":
			customMetadatas[field.Alias] = metadata.PodName
		case "PodNamespace":
			customMetadatas[field.Alias] = metadata.PodNamespace
		case "PodIP":
			customMetadatas[field.Alias] = metadata.PodIP
		case "TrustDomain":
			customMetadatas[field.Alias] = metadata.TrustDomain
		}
	}

	c := &KeyFactorCAClient{
		CaEndpoint:      endpoint,
		EnableTLS:       enableTLS,
		CustomMetadatas: customMetadatas,
		Metadata:        metadata,
		ClientOptions:   keyfactorConfig,
	}

	if !enableTLS {

		transport := &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
		c.Client = &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		}
	} else {

		// Load the system default root certificates.
		pool, err := x509.SystemCertPool()
		if err != nil {
			keyFactorCAClientLog.Errorf("Could not get SystemCertPool: %v", err)
			return nil, fmt.Errorf("Could not get SystemCertPool: %v", err)
		}

		if pool == nil {
			log.Info("System cert pool is nil, create a new cert pool")
			pool = x509.NewCertPool()
		}

		if len(rootCert) > 0 {
			ok := pool.AppendCertsFromPEM(rootCert)
			if !ok {
				return nil, fmt.Errorf("Invalid root-cert.pem: %v", string(rootCert))
			}
		}

		tlsConfig := &tls.Config{
			RootCAs: pool,
		}

		transport := &http.Transport{TLSClientConfig: tlsConfig}

		c.Client = &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		}
	}

	return c, nil
}

// CSRSign calls KeyFactor CA to sign a CSR.
func (cl *KeyFactorCAClient) CSRSign(ctx context.Context, reqID string, csrPEM []byte, subjectID string,
	certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error) {

	keyFactorCAClientLog.Infof("Start sign Keyfactor CSR request:")

	bytesRepresentation, err := json.Marshal(keyfactorRequestPayload{
		CSR:                  string(csrPEM),
		CertificateAuthority: cl.ClientOptions.CaName,
		IncludeChain:         true,
		Template:             cl.ClientOptions.CaTemplate,
		TimeStamp:            time.Now().Format(time.RFC3339),
		Metadata:             cl.CustomMetadatas,
		SANs: san{
			DNS: []string{cl.Metadata.ClusterID},
			IP4: []string{cl.Metadata.PodIP},
		},
	})

	u, err := url.Parse(cl.CaEndpoint)

	if err != nil {
		return nil, fmt.Errorf("Invalid caAddress: %v (%v)", cl.CaEndpoint, err)
	}

	u.Path = path.Join(u.Path, cl.ClientOptions.EnrollPath)
	enrollCSRPath := u.String()

	requestCSR, err := http.NewRequest("POST", enrollCSRPath, bytes.NewBuffer(bytesRepresentation))

	if err != nil {
		return nil, fmt.Errorf("Cannot create request with url: %v", enrollCSRPath)
	}

	requestCSR.Header.Set("authorization", cl.ClientOptions.AuthToken)
	requestCSR.Header.Set("x-keyfactor-requested-with", "APIClient")
	requestCSR.Header.Set("x-Keyfactor-appKey", cl.ClientOptions.AppKey)
	requestCSR.Header.Set("x-certificateformat", "PEM")
	requestCSR.Header.Set("Content-Type", "application/json")

	if err != nil {
		keyFactorCAClientLog.Errorf("Request to keyfactor is invalid: %v", err)
		return nil, fmt.Errorf("Request to keyfactor is invalid: %v", err)
	}

	res, err := cl.Client.Do(requestCSR)
	if err != nil {
		return nil, fmt.Errorf("Could not request to KeyfactorCA server: %v %v", cl.CaEndpoint, err)
	}
	defer res.Body.Close()
	status := res.StatusCode

	if status == http.StatusOK {
		jsonResponse := &KeyfactorResponse{}
		json.NewDecoder(res.Body).Decode(&jsonResponse)
		return getCertFromResponse(jsonResponse), nil
	}

	var errorMessage interface{}
	json.NewDecoder(res.Body).Decode(&errorMessage)
	keyFactorCAClientLog.Errorf("Request failed with status: %v, message: %v", status, errorMessage)
	return nil, fmt.Errorf("Request failed with status: %v, message: %v", status, errorMessage)
}

func getCertFromResponse(jsonResponse *KeyfactorResponse) []string {

	certChains := []string{}

	template := "-----BEGIN CERTIFICATE-----\n%s\n-----END CERTIFICATE-----\n"

	for _, i := range jsonResponse.CertificateInformation.Certificates {
		certChains = append(certChains, fmt.Sprintf(template, i))
	}

	keyFactorCAClientLog.Infof("Keyfactor response %v certificates in certchain.", len(certChains))

	return certChains
}
