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

package fuzz

import (
	"crypto/x509/pkix"
	"os"

	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/istio/security/pkg/pki/util"
)

// FuzzVerifyCertificate implements a fuzzer
// that tests util.VerifyCertificate
func FuzzVerifyCertificate(data []byte) int {
	f := fuzz.NewConsumer(data)
	privPem, err := f.GetBytes()
	if err != nil {
		return 0
	}
	certChainPem, err := f.GetBytes()
	if err != nil {
		return 0
	}
	rootCertPem, err := f.GetBytes()
	if err != nil {
		return 0
	}
	expectedFields := &util.VerifyFields{}
	err = f.GenerateStruct(expectedFields)
	if err != nil {
		return 0
	}
	util.VerifyCertificate(privPem, certChainPem, rootCertPem, expectedFields)
	return 1
}

// FindRootCertFromCertificateChainBytesFuzz implements a fuzzer
// that tests util.FindRootCertFromCertificateChainBytes
func FuzzFindRootCertFromCertificateChainBytes(data []byte) int {
	_, _ = util.FindRootCertFromCertificateChainBytes(data)
	return 1
}

func FuzzExtractIDs(data []byte) int {
	f := fuzz.NewConsumer(data)
	noOfExts, err := f.GetInt()
	if err != nil {
		return 0
	}
	noOfExtensions := noOfExts % 30
	if noOfExtensions == 0 {
		return 0
	}
	extensions := make([]pkix.Extension, 0)
	for i := 0; i < noOfExtensions; i++ {
		newExtension := pkix.Extension{}
		err = f.GenerateStruct(&newExtension)
		if err != nil {
			return 0
		}
		extensions = append(extensions, newExtension)
	}
	_, _ = util.ExtractIDs(extensions)
	return 1
}

// FuzzPemCertBytestoString implements a fuzzer
// that tests PemCertBytestoString
func FuzzPemCertBytestoString(data []byte) int {
	_ = util.PemCertBytestoString(data)
	return 1
}

// FuzzParsePemEncodedCertificateChain implements
// a fuzzer that tests ParsePemEncodedCertificateChain
func FuzzParsePemEncodedCertificateChain(data []byte) int {
	_, _, _ = util.ParsePemEncodedCertificateChain(data)
	return 1
}

// FuzzUpdateVerifiedKeyCertBundleFromFile implements a
// fuzzer that tests UpdateVerifiedKeyCertBundleFromFile
func FuzzUpdateVerifiedKeyCertBundleFromFile(data []byte) int {
	f := fuzz.NewConsumer(data)
	certFile, err := os.Create("certfile")
	if err != nil {
		return 0
	}
	defer certFile.Close()
	defer os.Remove("certfile")

	certFileBytes, err := f.GetBytes()
	if err != nil {
		return 0
	}
	_, err = certFile.Write(certFileBytes)
	if err != nil {
		return 0
	}

	privKeyFile, err := os.Create("privKeyFile")
	if err != nil {
		return 0
	}
	defer privKeyFile.Close()
	defer os.Remove("privKeyFile")

	privKeyFileBytes, err := f.GetBytes()
	if err != nil {
		return 0
	}
	_, err = privKeyFile.Write(privKeyFileBytes)
	if err != nil {
		return 0
	}

	certChainFile, err := os.Create("certChainFile")
	if err != nil {
		return 0
	}
	defer certChainFile.Close()
	defer os.Remove("certChainFile")

	certChainBytes, err := f.GetBytes()
	if err != nil {
		return 0
	}
	_, err = certChainFile.Write(certChainBytes)
	if err != nil {
		return 0
	}

	rootCertFile, err := os.Create("rootCertFile")
	if err != nil {
		return 0
	}
	defer rootCertFile.Close()
	defer os.Remove("rootCertFile")

	rootCertFileBytes, err := f.GetBytes()
	if err != nil {
		return 0
	}
	_, err = rootCertFile.Write(rootCertFileBytes)
	if err != nil {
		return 0
	}
	bundle, err := util.NewVerifiedKeyCertBundleFromFile("certfile", "privKeyFile", []string{"certChainFile"}, "rootCertFile", "")
	if err != nil {
		return 0
	}
	_, err = bundle.CertOptions()
	if err == nil {
		panic("Ran successfully")
	}

	newCertFile, err := os.Create("newCertfile")
	if err != nil {
		return 0
	}
	defer newCertFile.Close()
	defer os.Remove("newCertFile")

	newCertFileBytes, err := f.GetBytes()
	if err != nil {
		return 0
	}
	_, err = newCertFile.Write(newCertFileBytes)
	if err != nil {
		return 0
	}

	newPrivKeyFile, err := os.Create("newPrivKeyFile")
	if err != nil {
		return 0
	}
	defer newPrivKeyFile.Close()
	defer os.Remove("newPrivKeyFile")

	newPrivKeyFileBytes, err := f.GetBytes()
	if err != nil {
		return 0
	}
	_, err = newPrivKeyFile.Write(newPrivKeyFileBytes)
	if err != nil {
		return 0
	}

	newCertChainFile, err := os.Create("newCertChainFile")
	if err != nil {
		return 0
	}
	defer newCertChainFile.Close()
	defer os.Remove("newCertChainFile")

	newCertChainBytes, err := f.GetBytes()
	if err != nil {
		return 0
	}
	_, err = newCertChainFile.Write(newCertChainBytes)
	if err != nil {
		return 0
	}

	newRootCertFile, err := os.Create("newRootCertFile")
	if err != nil {
		return 0
	}
	defer newRootCertFile.Close()
	defer os.Remove("newRootCertFile")

	newRootCertFileBytes, err := f.GetBytes()
	if err != nil {
		return 0
	}
	_, err = newRootCertFile.Write(newRootCertFileBytes)
	if err != nil {
		return 0
	}

	bundle.UpdateVerifiedKeyCertBundleFromFile("newCertfile", "newPrivKeyFile", []string{"newCertChainFile"}, "newRootCertFile", "")
	return 1
}
