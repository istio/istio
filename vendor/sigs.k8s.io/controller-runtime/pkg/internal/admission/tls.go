/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package admission

//type certs struct {
//	Cert   []byte
//	Key    []byte
//	CACert []byte
//}

//// MakeTLSConfig makes a TLS configuration suitable for use with the server
//func makeTLSConfig(certs certs) (*tls.Config, error) {
//	caCertPool := x509.NewCertPool()
//	caCertPool.AppendCertsFromPEM(certs.CACert)
//	//cert, err := tls.X509KeyPair(certs.Cert, certs.Key)
//	//if err != nil {
//	//	return nil, err
//	//}
//	return &tls.Config{
//		//Certificates: []tls.Certificate{cert},
//		ClientCAs:  caCertPool,
//		ClientAuth: tls.NoClientCert,
//		// Note on GKE there apparently is no client cert sent, so this
//		// does not work on GKE.
//		// TODO: make this into a configuration option.
//		//		ClientAuth:   tls.RequireAndVerifyClientCert,
//	}, nil
//}
