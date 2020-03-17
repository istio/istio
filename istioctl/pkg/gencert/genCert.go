package genCert

import (
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"k8s.io/client-go/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/pki/ca"
)

const (
	// caCertID is the CA certificate chain file.
	caCertID = "ca-cert.pem"
	// caPrivateKeyID is the private key file of CA.
	caPrivateKeyID = "ca-key.pem"
	timeLayout   = "Jan 2 15:04:05 2006"
)

// GenerateCertKayAndExtractRootCert will generate key and certificate for workloads using citadel as CA and extract root cert out
func GenerateCertKayAndExtractRootCert(opts util.CertOptions, client kubernetes.Interface,ns string)([]byte,[]byte, []byte, error) {
	caSecret, scrtErr := client.CoreV1().Secrets(ns).Get(ca.CASecret, metav1.GetOptions{})
	if scrtErr != nil {
		return nil, nil, nil,scrtErr
	}
	signerCertBytes := caSecret.Data[caCertID]
	signerPrivBytes := caSecret.Data[caPrivateKeyID]
	signerCert, err := util.ParsePemEncodedCertificate(signerCertBytes)
	if err != nil {
		return nil, nil, nil,fmt.Errorf("pem encoded cert parsing failure (%v)", err)
	}
	signerPriv, err := util.ParsePemEncodedKey(signerPrivBytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("pem encoded key parsing failure (%v)", err)
	}
	opts.SignerCert = signerCert
	opts.SignerPriv = signerPriv
	certPem, privPem, err := util.GenCertKeyFromOptions(opts)
	if err != nil {
		return nil,nil,nil,fmt.Errorf("Failed to generate certificate: %v\n", err)
	}
	return certPem, privPem, signerCertBytes, nil
}

// GetNotBefore will return a timestamp which is not before current time or the time specified
func GetNotBefore(validFrom string) time.Time {
	if validFrom == "" {
		return time.Now()
	}

	t, err := time.Parse(timeLayout, validFrom)
	if err != nil {
		log.Fatalf("Failed to parse the '-start-from' option as a time (error: %s)\n", err)
	}

	return t
}

// SaveCreds will save root cert and generated cert and key into specified files
func SaveCreds(outCert, outPriv, outRootCert string, certPem []byte, privPem []byte, rootCert []byte) {
	err := ioutil.WriteFile(outCert, certPem, 0644)
	if err != nil {
		log.Fatalf("Could not write output certificate: %s.", err)
	}

	err = ioutil.WriteFile(outPriv, privPem, 0600)
	if err != nil {
		log.Fatalf("Could not write output private key: %s.", err)
	}
	if outRootCert == "" || rootCert == nil {
		return
	}
	err = ioutil.WriteFile(outRootCert, rootCert, 0600)
	if err != nil {
		log.Fatalf("Could not write output private key: %s.", err)
	}
}
