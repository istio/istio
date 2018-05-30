// Copyright 2018 Istio Authors
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

package registry

import (
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gogo/googleapis/google/rpc"
	"golang.org/x/net/context"

	"istio.io/istio/security/cmd/node_agent_k8s/workload/handler"
	wapi "istio.io/istio/security/cmd/node_agent_k8s/workloadapi"
	"istio.io/istio/security/pkg/caclient"
	"istio.io/istio/security/pkg/nodeagent/secrets"
	nvm "istio.io/istio/security/pkg/nodeagent/vm"
	pkiutil "istio.io/istio/security/pkg/pki/util"
	pb "istio.io/istio/security/proto"
)

type certFiles struct {
	dir           string
	rootFile      string
	keyFile       string
	certChainFile string
	bundle        pkiutil.KeyCertBundle
}

func setupCertFiles(t *testing.T) (*certFiles, func()) {
	t.Helper()
	dir, err := ioutil.TempDir("./", "management_server")
	if err != nil {
		t.Errorf("failed to setup temp dir")
	}
	rootCAOpts := pkiutil.CertOptions{
		IsCA:         true,
		IsSelfSigned: true,
		TTL:          time.Hour,
		Org:          "Root CA",
		RSAKeySize:   2048,
	}
	rootCertBytes, rootKeyBytes, err := pkiutil.GenCertKeyFromOptions(rootCAOpts)
	if err != nil {
		t.Error(err)
	}
	kb, err := pkiutil.NewVerifiedKeyCertBundleFromPem(rootCertBytes, rootKeyBytes, rootCertBytes, rootCertBytes)
	if err != nil {
		t.Errorf("failed to create testing keycert bundle %v", err)
	}
	cf := &certFiles{
		dir:           dir,
		rootFile:      filepath.Join(dir, "root-cert.pem"),
		keyFile:       filepath.Join(dir, "priv.pem"),
		certChainFile: filepath.Join(dir, "cert-chain.pem"),
		bundle:        kb,
	}
	if err := ioutil.WriteFile(cf.rootFile, rootCertBytes, secrets.CertFilePermission); err != nil {
		t.Errorf("failed to write testing root-cert into file %v err %v", cf.rootFile, err)
	}
	if err := ioutil.WriteFile(cf.keyFile, rootKeyBytes, secrets.KeyFilePermission); err != nil {
		t.Errorf("failed to write private key file %v error %v", cf.keyFile, err)
	}
	if err := ioutil.WriteFile(cf.certChainFile, rootCertBytes, secrets.CertFilePermission); err != nil {
		t.Errorf("failed to write cert chain file %v error %v", cf.certChainFile, err)
	}
	return cf, func() {
		_ = os.RemoveAll(dir)
	}
}

type fakeRetriever struct {
	signer    pkiutil.KeyCertBundle
	bundleMap map[string]pkiutil.KeyCertBundle
	errMsg    string
}

func (f *fakeRetriever) setupWorkload(host string) error {
	rootCert, rootKey, certChain, rootCertBytes := f.signer.GetAll()
	csrPEM, priv, err := pkiutil.GenCSR(pkiutil.CertOptions{
		Host:       host,
		RSAKeySize: 1024,
		IsCA:       false,
	})
	if err != nil {
		return err
	}
	csr, err := pkiutil.ParsePemEncodedCSR(csrPEM)
	if err != nil {
		return err
	}
	certBytes, err := pkiutil.GenCertFromCSR(csr, rootCert, csr.PublicKey, *rootKey, time.Hour, false)
	if err != nil {
		return err
	}
	cert := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})
	kb, err := pkiutil.NewVerifiedKeyCertBundleFromPem(cert, priv, certChain, rootCertBytes)
	if err != nil {
		return err
	}
	f.bundleMap[host] = kb
	return nil
}

func (f *fakeRetriever) Retrieve(opt *pkiutil.CertOptions) (newCert, certChain, privateKey []byte, err error) {
	if f.errMsg != "" {
		return nil, nil, nil, fmt.Errorf(f.errMsg)
	}
	kb, ok := f.bundleMap[opt.Host]
	if !ok {
		return nil, nil, nil, fmt.Errorf("not found in the fake retriever %v", opt.Host)
	}
	cert, priv, chain, _ := kb.GetAllPem()
	return cert, chain, priv, nil
}

func TestWorkloadAddedService(t *testing.T) {
	cf, cleanup := setupCertFiles(t)
	defer cleanup()
	fake := &fakeRetriever{
		signer:    cf.bundle,
		bundleMap: make(map[string]pkiutil.KeyCertBundle),
	}
	if err := fake.setupWorkload("spiffe://cluster.local/ns/ns1/sa/sa1"); err != nil {
		t.Error(err)
	}
	server, err := New(&nvm.Config{
		WorkloadOpts: handler.Options{
			SockFile: "path",
			RegAPI:   wapi.RegisterGrpc,
		},
		CAClientConfig: caclient.Config{
			RootCertFile:  cf.rootFile,
			CertChainFile: cf.certChainFile,
			KeyFile:       cf.keyFile,
			RSAKeySize:    1024,
			Env:           "onprem",
		},
		SecretDirectory: cf.dir,
	}, fake)
	testCases := []struct {
		id     string
		attr   *pb.WorkloadInfo_WorkloadAttributes
		status rpc.Status
	}{
		{
			id:     "WorkloadAdded Success",
			attr:   &pb.WorkloadInfo_WorkloadAttributes{Uid: "uid-foo", Serviceaccount: "sa1", Namespace: "ns1"},
			status: rpc.Status{Code: int32(rpc.OK), Message: "OK"},
		},
		{
			id:     "WorkloadAdded already exists",
			attr:   &pb.WorkloadInfo_WorkloadAttributes{Uid: "uid-foo", Serviceaccount: "sa1", Namespace: "ns1"},
			status: rpc.Status{Code: int32(rpc.ALREADY_EXISTS), Message: "Already exists"},
		},
	}
	for _, tc := range testCases {
		if err != nil {
			t.Errorf("[%v]: failed to create NodeAgent server %v", tc.id, err)
		}
		resp, err := server.WorkloadAdded(context.Background(), &pb.WorkloadInfo{Attrs: tc.attr})
		if err != nil {
			t.Errorf("[%v] failed to add workload %v", tc.id, err)
		}
		fmt.Println("respon ", resp, " ", tc.id)
		if !reflect.DeepEqual(*resp.Status, tc.status) {
			t.Errorf("[%v] expect status (%v) got (%v)", tc.id, tc.status, *resp.Status)
		}
	}
}

func TestWorkloadDeletedService(t *testing.T) {
	server := &Server{
		handlerMap: map[string]handler.WorkloadHandler{},
		done:       make(chan bool),
		config: &nvm.Config{
			WorkloadOpts: handler.Options{
				SockFile: "path",
				RegAPI:   wapi.RegisterGrpc,
			},
			CAClientConfig: caclient.Config{},
		},
	}
	server.handlerMap["testid"] = nil

	attrs := pb.WorkloadInfo_WorkloadAttributes{Uid: "testid2"}
	naInp := &pb.WorkloadInfo{Attrs: &attrs}

	resp, err := server.WorkloadDeleted(context.Background(), naInp)
	if err != nil {
		t.Errorf("Failed to WorkloadDeleted.")
	}
	if resp.Status.Code != int32(rpc.NOT_FOUND) {
		t.Errorf("Failed to WorkloadDeleted with resp %v.", resp)
	}
}
