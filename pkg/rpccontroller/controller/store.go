package controller

import (
	"github.com/coreos/etcd/clientv3"

	"istio.io/istio/pkg/log"

	"io/ioutil"
	"time"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
)

type Store interface {
	Put(service string, data string) error
	Get(k string) (string, error)
}

// implement Store interface
type EtcdStore struct {
	Client *clientv3.Client
}

var (
	dialTimeout = 5 * time.Second
	requestTimeout = 2 * time.Second
	etcdPrefix = "/rpc-service-data/"
)

func NewEtcdStore(config *Config) *EtcdStore {
	return &EtcdStore{
		Client:NewEtcdClient(config),
	}
}

func NewEtcdClient(config *Config) *clientv3.Client {
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
		Endpoints:      config.EtcdEndpoints,
		TLS : tlsConfig,
		DialTimeout:    dialTimeout,
	})
	if err != nil {
		log.Errora("new client v3 err:%v", err)
		return nil
	}

	return client
}

func (es *EtcdStore) Put(service string, data string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	resp, err := es.Client.Put(ctx, etcdPrefix + service, data)
	cancel()
	if err != nil {
		log.Errorf("etcd put %s err: %v", service, err)
		return err
	}

	log.Infof("put %s:%s, resp:%v", service, data, resp)
	return err
}

func (es *EtcdStore) Get(k string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	resp, err := es.Client.Get(ctx, etcdPrefix + k)
	cancel()
	if err != nil {
		log.Errorf("etcd get %s err: %v", k, err)
		return "",err
	}

	if resp == nil || len(resp.Kvs) == 0 {
		return "", fmt.Errorf("etcd return empty response")
	}
	log.Infof("get %s, resp:%v", k, resp)

	return string(resp.Kvs[0].Value), nil
}