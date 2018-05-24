// Copyright 2018 Aspen Mesh Authors
//
// No part of this software may be reproduced or transmitted in any
// form or by any means, electronic or mechanical, for any purpose,
// without express written permission of F5 Networks, Inc.

package proxyexporter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"testing"
)

var parsetests = []struct {
	str  string
	info EnvoyCertInfo
	ok   bool
}{
	{`"Certificate Path: /etc/certs/root-cert.pem, Serial Number: f, Days until Expiration: 362"`,
		EnvoyCertInfo{"/etc/certs/root-cert.pem", big.NewInt(15), 362},
		true,
	},
	{`"Certificate Path: /etc/certs/root-cert.pem, New Field: XXXXX, Serial Number: f, Days until Expiration: 362"`,
		EnvoyCertInfo{"/etc/certs/root-cert.pem", big.NewInt(15), 362},
		true,
	},
	{`"Serial Number: f, Days until Expiration: 362"`,
		EnvoyCertInfo{"", big.NewInt(15), 362},
		false,
	},
	// Not quoted
	{`Certificate Path: /etc/certs/root-cert.pem, Serial Number: f, Days until Expiration: 362`,
		EnvoyCertInfo{},
		false,
	},
}

var marshaltests = []struct {
	str  string
	info EnvoyCertInfo
	ok   bool
}{
	{`"Certificate Path: /etc/certs/root-cert.pem, Serial Number: f, Days until Expiration: 362"`,
		EnvoyCertInfo{"/etc/certs/root-cert.pem", big.NewInt(15), 362},
		true,
	},
}

func TestCertInfoMarshal(t *testing.T) {
	for _, tt := range marshaltests {
		t.Run(tt.str, func(t *testing.T) {
			want := tt.str
			bytes, err := json.Marshal(&tt.info)
			if !tt.ok {
				if err == nil {
					t.Fatalf("Marshal should have failed %+v", tt.info)
				}
			} else {
				if err != nil {
					t.Fatalf("Error marshal %v: %v", tt.info, err)
				}
				if string(bytes) != want {
					t.Errorf("got '%s', want '%s'", bytes, want)
				}
			}
		})
	}
}

func TestCertMessageUnmarshal(t *testing.T) {
	for _, tt := range parsetests {
		t.Run(fmt.Sprintf("%+v", tt.info), func(t *testing.T) {
			bytes := []byte(`{"cert_chain": ` + tt.str + `}`)
			msg := EnvoyCertResponse{}
			t.Logf("STR %s", bytes)
			t.Logf("INFO %+v", tt.info)
			err := json.Unmarshal(bytes, &msg)
			if !tt.ok {
				if err == nil {
					t.Fatalf("Unmarshal should have failed %s", bytes)
				}
			} else {
				if err != nil {
					t.Fatalf("Error unmarshal %s: %v", bytes, err)
				}
				if !infoEQ(msg.CertChain, tt.info) {
					t.Errorf("got %+v, want %+v", msg, tt.info)
				}
			}
		})
	}
}

func TestCertInfoUnmarshal(t *testing.T) {
	for _, tt := range parsetests {
		t.Run(fmt.Sprintf("%+v", tt.info), func(t *testing.T) {
			bytes := []byte(tt.str)
			info := EnvoyCertInfo{}
			t.Logf("STR %s", bytes)
			t.Logf("INFO %+v", tt.info)
			err := json.Unmarshal(bytes, &info)
			if !tt.ok {
				if err == nil {
					t.Fatalf("Unmarshal should have failed %s", bytes)
				}
			} else {
				if err != nil {
					t.Fatalf("Error unmarshal %s: %v", bytes, err)
				}
				if !infoEQ(info, tt.info) {
					t.Errorf("got %+v, want %+v", info, tt.info)
				}
			}
		})
	}
}

func infoEQ(a, b EnvoyCertInfo) bool {
	return a.Path == b.Path && a.SerialNumber.Cmp(b.SerialNumber) == 0 && a.DaysUntilExpire == b.DaysUntilExpire
}

var certs1Msg = `{
"ca_cert": "Certificate Path: ./testdata/certs1/root-cert.pem, Serial Number: 23c8302f67670972d7ef7254665f1684, Days until Expiration: 357",
"cert_chain": "Certificate Path: ./testdata/certs1/cert-chain.pem, Serial Number: 122da1c5965963ce7f97fa1d75507111, Days until Expiration: 82"
}
`

func TestCertFileParse(t *testing.T) {
	certResp := EnvoyCertResponse{}
	err := json.Unmarshal([]byte(certs1Msg), &certResp)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	c := NewProxyCertInfo("")

	err = c.handleCertsResponse(&certResp)
	if err != nil {
		t.Fatalf("Failed to handle certs: %v", err)
	}

	rootBytes, err := ioutil.ReadFile(certResp.CACert.Path)
	if err != nil {
		t.Fatalf("Failed to get ca bytes: %v", err)
	}
	chainBytes, err := ioutil.ReadFile(certResp.CertChain.Path)
	if err != nil {
		t.Fatalf("Failed to get chain bytes: %v", err)
	}

	if !bytes.Equal(c.RootPem, rootBytes) {
		t.Fatalf("Wrong root bytes saved")
	}
	if !bytes.Equal(c.ChainPem, chainBytes) {
		t.Fatalf("Wrong chain bytes saved")
	}

	chainFP := "e0d27dd54fb938e889dbdcfe6a4075ebb7b9d6ded0b4c45bc3f61ea36c4cd95a"
	fp, ok := c.fingerprints[chainFP]
	if !ok || !fp {
		t.Fatalf("Expected fingerprint not active")
	}
}

func TestMultiCertFileParse(t *testing.T) {
	path := "./testdata/multicert.pem"

	c := NewProxyCertInfo("")
	parsedBytes, certList, _, err := c.readCertFile(path)
	if err != nil {
		t.Fatalf("Failed to parse cert file: %v", err)
	}

	expectedBytes, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to get multicert bytes: %v", err)
	}

	if !bytes.Equal(parsedBytes, expectedBytes) {
		t.Fatalf("Wrong bytes saved")
	}

	fingerprints := []string{
		"e0d27dd54fb938e889dbdcfe6a4075ebb7b9d6ded0b4c45bc3f61ea36c4cd95a",
		"abd85b4d3cb158edc8f6e6b374a47c290c19857c0a8caebe8f07f675d882724c",
	}
	if len(certList) != len(fingerprints) {
		t.Fatalf("Expected %d certs, got %d", len(fingerprints), len(certList))
	}

	for i := range fingerprints {
		fp := x509Fingerprint(certList[i])
		if fp != fingerprints[i] {
			t.Errorf("Expected fingerprint %d to be %s, got %s", i, fingerprints[i], fp)
		}
	}
}
