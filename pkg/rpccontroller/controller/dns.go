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
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/coreos/etcd/clientv3"
	"istio.io/istio/pkg/log"
)

const (
	skyDnsPrefix = "/skydns"
	defaultTTL   = 3600
)

type HostData struct {
	Host string `json:"host"`
	TTL  int    `json:"ttl"`
}

type DNSInterface interface {
	Update(domain, clusterIp, suffix string) error
	Delete(domain, suffix string) error
}

type CoreDNS struct {
	Client *clientv3.Client
}

func NewCoreDNS(client *clientv3.Client) *CoreDNS {
	return &CoreDNS{
		Client: client,
	}
}

func convertDomainToKey(domain string) string {
	keys := strings.Split(domain, ".")

	key := skyDnsPrefix
	for i := len(keys) - 1; i >= 0; i -= 1 {
		key += "/" + keys[i]
	}

	return strings.ToLower(key)
}

func (cd *CoreDNS) Update(domain, clusterIp, suffix string) error {
	key := convertDomainToKey(domain + suffix)

	hostData := HostData{
		Host: clusterIp,
		TTL:  defaultTTL,
	}
	data, _ := json.Marshal(&hostData)
	log.Infof("put <%s, %s>", key, string(data))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	_, err := cd.Client.Put(ctx, key, string(data))
	cancel()
	if err != nil {
		log.Errorf("put %s %s error: %v", key, string(data), err)
	}
	return err
}

func (cd *CoreDNS) Delete(domain, suffix string) error {
	key := convertDomainToKey(domain + suffix)
	log.Infof("delete %s", key)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	_, err := cd.Client.Delete(ctx, key)
	cancel()
	return err
}
