// Copyright 2018 Aspen Mesh Authors
//
// No part of this software may be reproduced or transmitted in any
// form or by any means, electronic or mechanical, for any purpose,
// without express written permission of F5 Networks, Inc.

package proxyexporter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DataDog/datadog-go/statsd"

	"istio.io/istio/pkg/log"
)

// ProxyCertInfo tracks the list of certificates being used by envoy
//
// It can export this status to a REST and statsd consumers
type ProxyCertInfo struct {
	RootPem          []byte
	ChainPem         []byte
	ChainFingerprint string

	client       *http.Client
	fingerprints map[string]bool
	certURL      string
	agentAddress string
}

// NewProxyCertInfo creates a new default ProxyCertInfo
func NewProxyCertInfo(agentAddress string) *ProxyCertInfo {
	return &ProxyCertInfo{
		agentAddress: agentAddress,
		fingerprints: make(map[string]bool),
		certURL:      "http://localhost:15000/certs",
		client:       &http.Client{Timeout: time.Second * 10},
	}
}

// Export fetches status from envoy certs admin endpoint, and updates local state
func (c *ProxyCertInfo) Export(ctx context.Context, statsc *statsd.Client) error {

	err := c.Update(ctx)
	if err != nil {
		// Keep exporting stats when there is an error
		log.Warnf("Failed to update certs: %v", err)
	}

	err = c.sendMetrics(ctx, statsc)
	if err != nil {
		return err
	}
	err = c.sendCerts(ctx)

	return err
}

// Finalize performs any final export needed before process exits
func (c *ProxyCertInfo) Finalize(ctx context.Context, statsc *statsd.Client) error {
	// Send stats one last time with all zeros
	c.setInuse([]string{})
	c.RootPem = nil
	c.ChainPem = nil

	return c.sendMetrics(ctx, statsc)
}

// Update fetches state from envoy admin port, and updates state.
func (c *ProxyCertInfo) Update(ctx context.Context) error {
	certsInfo, err := c.getCertsInfo(ctx)
	if err != nil {
		// Clear the state that we report
		c.setInuse([]string{})
		c.RootPem = nil
		c.ChainPem = nil
		return err
	}

	return c.handleCertsResponse(certsInfo)
}

func (c *ProxyCertInfo) sendMetrics(ctx context.Context, statsc *statsd.Client) error {
	var err error
	for fp := range c.fingerprints {
		tags := []string{"fingerprint:" + fp}

		var val float64
		if c.fingerprints[fp] {
			val = 1
		} else {
			val = 0
		}

		err = statsc.Gauge("inuse_count", val, tags, 1)
	}
	return err
}

// PutCertMsg represntes the sidecarcerts mesage to send to agent
type PutCertMsg struct {
	PemString string `json:"pem_string"`
}

func (c *ProxyCertInfo) sendCerts(ctx context.Context) error {
	if c.ChainPem == nil {
		return nil
	}

	msg := PutCertMsg{string(c.ChainPem)}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s/v1/status.aspenmesh.io/sidecarcerts/%s", c.agentAddress, c.ChainFingerprint)
	req, err := http.NewRequest("PUT", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	req.ContentLength = int64(len(data))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != 200 {
		msg, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("error saving cert contents: %s: %s", resp.Status, msg)
	}

	return nil
}

// Update the set of cert fingerprint in use
func (c *ProxyCertInfo) setInuse(fingerprints []string) {
	for fp := range c.fingerprints {
		c.fingerprints[fp] = false
	}
	for _, fp := range fingerprints {
		c.fingerprints[fp] = true
	}
}

// Read the certs from the response, and update state
func (c *ProxyCertInfo) handleCertsResponse(resp *EnvoyCertResponse) error {

	// Create cert pools from root, so we can verify the cert used by envoy
	rootPem, _, _, err := c.readCertFile(resp.CACert.Path)
	if err != nil {
		return err
	}

	chainPem, chainCerts, _, err := c.readCertFile(resp.CertChain.Path)
	if err != nil {
		return err
	}

	for _, cert := range chainCerts {
		if cert.SerialNumber.Cmp(resp.CertChain.SerialNumber) == 0 {
			// This is the cert that envoy is referencing
			// The serial number isn't perfect. Maybe also verify SAN?

			fp := x509Fingerprint(cert)
			c.setInuse([]string{fp})
			c.RootPem = rootPem
			c.ChainPem = chainPem
			c.ChainFingerprint = fp
			return nil
		}
	}

	return fmt.Errorf("couldn't find certificate with serial number %s", resp.CertChain.SerialNumber.Text(16))
}

// Fetch the /certs enpoint from envoy admin port
func (c *ProxyCertInfo) getCertsInfo(ctx context.Context) (*EnvoyCertResponse, error) {
	req, err := http.NewRequest("GET", c.certURL, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	certs := &EnvoyCertResponse{}
	err = json.NewDecoder(resp.Body).Decode(certs)
	if err != nil {
		return nil, err
	}

	return certs, nil
}

func (c *ProxyCertInfo) readCertFile(path string) ([]byte, []*x509.Certificate, *x509.CertPool, error) {

	pemData, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("can't read cert file %s: %s", path, err.Error())
	}

	certs := []*x509.Certificate{}
	pool := x509.NewCertPool()
	rest := pemData
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" {
			return nil, nil, nil, fmt.Errorf("failed to decode PEM block containing certificate")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to parse certificate")
		}

		certs = append(certs, cert)
		pool.AddCert(cert)

		if len(rest) == 0 {
			break
		}
	}

	return pemData, certs, pool, nil
}

func x509Fingerprint(cert *x509.Certificate) string {
	h := sha256.New()
	_, err := h.Write(cert.Raw)
	if err != nil {
		return ""
	}
	sumBytes := h.Sum(nil)
	return hex.EncodeToString(sumBytes)
}

// EnvoyCertResponse represents the response from envoy's admin /certs endpoint
type EnvoyCertResponse struct {
	CACert    EnvoyCertInfo `json:"ca_cert"`
	CertChain EnvoyCertInfo `json:"cert_chain"`
}

// EnvoyCertInfo represents the cert message returned from envoy's admin /certs endpoint
// There is a custom marshaller.
// The format looks like this:
// Certificate Path: /etc/certs/cert-chain.pem, Serial Number: 122da1c5965963ce7f97fa1d75507111, Days until Expiration: 87
type EnvoyCertInfo struct {
	Path            string
	SerialNumber    *big.Int
	DaysUntilExpire int
}

// UnmarshalJSON implements json.Unmarshaler interface
func (i *EnvoyCertInfo) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}

	keyvals := strings.Split(str, ", ")

	infoMap := map[string]string{}

	for i := range keyvals {
		parts := strings.Split(keyvals[i], ": ")
		if len(parts) != 2 {
			return fmt.Errorf("can't parse key-value %s", keyvals[i])
		}
		infoMap[parts[0]] = parts[1]
	}

	path, ok := infoMap["Certificate Path"]
	if !ok {
		return fmt.Errorf("missing 'Ceritifcate Path'")
	}
	i.Path = path

	serNoStr, ok := infoMap["Serial Number"]
	if !ok {
		return fmt.Errorf("missing 'Serial Number'")
	}
	serNo := &big.Int{}
	_, ok = serNo.SetString(serNoStr, 16)
	if !ok {
		return fmt.Errorf("invalid 'Serial Number'")
	}
	i.SerialNumber = serNo

	days, ok := infoMap["Days until Expiration"]
	if !ok {
		return fmt.Errorf("missing ceritifcate path")
	}
	var err error
	i.DaysUntilExpire, err = strconv.Atoi(days)
	if err != nil {
		return fmt.Errorf("days is not integer: %v", err)
	}

	return nil
}

// MarshalJSON implemented Marshaler interface
func (i EnvoyCertInfo) MarshalJSON() ([]byte, error) {
	str := fmt.Sprintf("Certificate Path: %s, Serial Number: %s, Days until Expiration: %d",
		i.Path, i.SerialNumber.Text(16), i.DaysUntilExpire)

	return json.Marshal(str)
}
