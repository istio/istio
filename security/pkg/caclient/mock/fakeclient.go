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

package mock

// FakeClient is a mocked Client.
type FakeClient struct {
	NewCert    []byte
	CertChain  []byte
	PrivateKey []byte
	Err        error
}

// RetrieveNewKeyCert returns error if Err is not nil, otherwise it returns NewCert, CertChain and PrivateKey.
func (c *FakeClient) RetrieveNewKeyCert() (newCert []byte, certChain []byte, privateKey []byte, err error) {
	if c.Err != nil {
		return nil, nil, nil, c.Err
	}
	return c.NewCert, c.CertChain, c.PrivateKey, nil
}
