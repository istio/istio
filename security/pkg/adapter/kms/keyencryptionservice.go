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

package kms

import (
	"context"
	"encoding/pem"
	"errors"
	"time"

	"github.com/thalescpl-io/k8s-kms-plugin/apis/istio/v1"

	"istio.io/pkg/log"
)

var kmsLog = log.RegisterScope("kms", "KMS adapter log", 0)

// KeyType is the type of the key.
type KeyType int

const (
	// AES is the Advanced Encryption Standard.
	AES KeyType = iota
	// RSA is the Rivest–Shamir–Adleman encryption.
	RSA
	// ECC is the Elliptic-curve cryptography.
	ECC
)

// KeyEncryptionService is the adapter to interact with the KMS.
type KeyEncryptionService struct {
	// The KMS endpoint
	// No security configuration is needed as we only support local UDS now.
	Endpoint string

	ctx    context.Context
	Cancel context.CancelFunc
	c      istio.KeyManagementServiceClient
}

func (kes *KeyEncryptionService) Connect(timeout time.Duration) error {
	var err error

	kes.ctx, kes.Cancel, kes.c, err = istio.GetClientSocket(kes.Endpoint, timeout)
	if err != nil {
		kmsLog.Errorf("Client socket failed: %v", err)
	}
	return err
}

// GenerateDEK gets the encrypted target key blob.
func (kes *KeyEncryptionService) GenerateDEK(kekID []byte) (encDEK []byte, err error) {
	var genDEKResp *istio.GenerateDEKResponse
	if genDEKResp, err = kes.c.GenerateDEK(kes.ctx, &istio.GenerateDEKRequest{
		KekKid: kekID,
	}); err != nil {
		kmsLog.Errorf(err)

		return nil, err
	}

	return genDEKResp.EncryptedDekBlob, nil
}

// Convert the KES KeyType into a the KMS's KeyKind.
func getKeyKind(keyType KeyType) istio.KeyKind {
	switch keyType {
	case AES:
		return istio.KeyKind_AES
	case RSA:
		return istio.KeyKind_RSA
	case ECC:
		return istio.KeyKind_ECC
	default:
		return istio.KeyKind_UNKNOWN
	}
}

// GenerateSkey gets the encrypted target key blob.
func (kes *KeyEncryptionService) GenerateSKey(kekID []byte, encDEK []byte, keySize int, keyType KeyType) (
	encSKey []byte, err error) {
	keyKind := getKeyKind(keyType)

	var genSKeyResp *istio.GenerateSKeyResponse
	if genSKeyResp, err = kes.c.GenerateSKey(kes.ctx, &istio.GenerateSKeyRequest{
		Size:             int64(keySize),
		Kind:             keyKind,
		KekKid:           kekID,
		EncryptedDekBlob: encDEK,
	}); err != nil {
		kmsLog.Errorf(err)
		return nil, err
	}

	return genSKeyResp.EncryptedSkeyBlob, nil
}

// GetKey gets the decrypted target key.
func (kes *KeyEncryptionService) GetSKey(kekID, encDEK, encSKey []byte) (decSKey []byte, err error) {
	var loadSKeyResp *istio.LoadSKeyResponse
	if loadSKeyResp, err = kes.c.LoadSKey(kes.ctx, &istio.LoadSKeyRequest{
		KekKid:            kekID,
		EncryptedDekBlob:  encDEK,
		EncryptedSkeyBlob: encSKey,
	}); err != nil {
		kmsLog.Errorf(err)
		return nil, err
	}

	return loadSKeyResp.PlaintextSkey, nil
}

// AutenticatedEncrypt encrypts with AEAD 256-bit AES.
func (kes *KeyEncryptionService) AuthenticatedEncrypt(kekID, encDEK, aad, plaintext []byte) (
	ciphertext []byte, err error) {
	var aeResp *istio.AuthenticatedEncryptResponse
	if aeResp, err = kes.c.AuthenticatedEncrypt(kes.ctx, &istio.AuthenticatedEncryptRequest{
		KekKid:           kekID,
		EncryptedDekBlob: encDEK,
		Aad:              aad,
		Plaintext:        plaintext,
	}); err != nil {
		kmsLog.Errorf(err)
		return nil, err
	}

	return aeResp.Ciphertext, nil
}

// AutenticatedDecrypt decrypts with AEAD 256-bit AES.
func (kes *KeyEncryptionService) AuthenticatedDecrypt(kekID, encDEK, aad, ciphertext []byte) (
	plaintext []byte, err error) {
	var adResp *istio.AuthenticatedDecryptResponse
	if adResp, err = kes.c.AuthenticatedDecrypt(kes.ctx, &istio.AuthenticatedDecryptRequest{
		KekKid:           kekID,
		EncryptedDekBlob: encDEK,
		Aad:              aad,
		Ciphertext:       ciphertext,
	}); err != nil {
		kmsLog.Errorf(err)
		return nil, err
	}

	return adResp.Plaintext, nil
}

// VerifyCertChain verifies a certificate chain in PEM format with a CA cert that's been
// loaded into the HSM.
func (kes *KeyEncryptionService) VerifyCertChain(certChain []byte) (success bool, err error) {
	block, _ := pem.Decode(certChain)
	if block == nil || block.Type != "CERTIFICATE" {
		return false, errors.New("failed to decode PEM block containing certificate")
	}

	chain := make([][]byte, 0)
	chain = append(chain, block.Bytes)

	verifyCertChainReq := &istio.VerifyCertChainRequest{
		Certificates: chain,
	}
	var verifyCertChainResp *istio.VerifyCertChainResponse
	if verifyCertChainResp, err = kes.c.VerifyCertChain(kes.ctx, verifyCertChainReq); nil != err {
		kmsLog.Errorf(err)
		return false, err
	}

	return verifyCertChainResp.SuccessfulVerification, nil
}
