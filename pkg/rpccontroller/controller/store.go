/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controller

import (
	"github.com/coreos/etcd/clientv3"

	"istio.io/istio/pkg/log"

	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"time"
)

var (
	dialTimeout    = 5 * time.Second
)

func newEtcdClient(config *Config) *clientv3.Client {
	cert, err := tls.LoadX509KeyPair(config.EtcdCertFile, config.EtcdKeyFile)
	if err != nil {
		log.Errora("LoadX509KeyPair err:%v", err)
		return nil
	}

	// Load CA cert
	caCert, err := ioutil.ReadFile(config.EtcdCaCertFile)
	if err != nil {
		log.Errora("ReadFile err:%v", err)
		return nil
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	// Setup HTTPS client
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}
	tlsConfig.BuildNameToCertificate()
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   config.EtcdEndpoints,
		TLS:         tlsConfig,
		DialTimeout: dialTimeout,
	})
	if err != nil {
		log.Errora("new client v3 err:%v", err)
		return nil
	}

	return client
}
