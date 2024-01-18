.SUFFIXES: .csr .pem .conf
.PRECIOUS: %/ca-key.pem %/ca-cert.pem %/cert-chain.pem
.PRECIOUS: %/workload-cert.pem %/key.pem %/workload-cert-chain.pem
.SECONDARY: root-cert.csr root-ca.conf %/cluster-ca.csr %/intermediate.conf

.DEFAULT_GOAL := help

SELF_DIR := $(dir $(lastword $(MAKEFILE_LIST)))

include $(SELF_DIR)common.mk

#------------------------------------------------------------------------
##help:		print this help message
.PHONY: help

help:
	@fgrep -h "##" $(MAKEFILE_LIST) | fgrep -v fgrep | sed -e 's/##//'

#------------------------------------------------------------------------
##fetch-root-ca:	fetch root CA  and key from a k8s cluster.
.PHONY: fetch-root-ca
rawcluster := $(shell kubectl config current-context)
cluster := $(subst /,-,$(rawcluster))
pwd := $(shell pwd)
export KUBECONFIG

fetch-root-ca:
	@echo "fetching root ca from k8s cluster: "$(cluster)""
	@mkdir -p $(pwd)/$(cluster)
	@res=$$(kubectl get secret istio-ca-secret -n $(ISTIO_NAMESPACE) >/dev/null 2>&1; echo $$?); \
	if [ $$res -eq 1 ]; then \
		kubectl get secret cacerts -n $(ISTIO_NAMESPACE) -o "jsonpath={.data['ca-cert\.pem']}" | base64 -d > $(cluster)/k8s-root-cert.pem; \
		kubectl get secret cacerts -n $(ISTIO_NAMESPACE) -o "jsonpath={.data['ca-key\.pem']}" | base64 -d > $(cluster)/k8s-root-key.pem; \
	else \
		kubectl get secret istio-ca-secret -n $(ISTIO_NAMESPACE) -o "jsonpath={.data['ca-cert\.pem']}" | base64 -d > $(cluster)/k8s-root-cert.pem; \
		kubectl get secret istio-ca-secret -n $(ISTIO_NAMESPACE) -o "jsonpath={.data['ca-key\.pem']}" | base64 -d > $(cluster)/k8s-root-key.pem; \
	fi

k8s-root-cert.pem:
	@cat $(cluster)/k8s-root-cert.pem > $@

k8s-root-key.pem:
	@cat $(cluster)/k8s-root-key.pem > $@
#------------------------------------------------------------------------
##<name>-cacerts: generate intermediate certificates for a cluster or VM with <name> signed with istio root cert from the specified k8s cluster and store them under <name> directory
.PHONY: %-cacerts

%-cacerts: %/cert-chain.pem
	@echo "done"

%/cert-chain.pem: %/ca-cert.pem k8s-root-cert.pem
	@echo "generating $@"
	@cat $^ > $@
	@echo "Intermediate certs stored in $(dir $<)"
	@cp k8s-root-cert.pem $(dir $<)/root-cert.pem

%/ca-cert.pem: %/cluster-ca.csr k8s-root-key.pem k8s-root-cert.pem
	@echo "generating $@"
	@openssl x509 -req -days $(INTERMEDIATE_DAYS) \
		-CA k8s-root-cert.pem -CAkey k8s-root-key.pem -CAcreateserial\
		-extensions req_ext -extfile $(dir $<)/intermediate.conf \
		-in $< -out $@

%/cluster-ca.csr: L=$(dir $@)
%/cluster-ca.csr: %/ca-key.pem %/intermediate.conf
	@echo "generating $@"
	@openssl req -new -config $(L)/intermediate.conf -key $< -out $@

%/ca-key.pem: fetch-root-ca
	@echo "generating $@"
	@mkdir -p $(dir $@)
	@openssl genrsa -out $@ 4096

#------------------------------------------------------------------------
##<namespace>-certs: generate intermediate certificates and sign certificates for a virtual machine connected to the namespace `<namespace> using serviceAccount `$SERVICE_ACCOUNT` using root cert from k8s cluster.
.PHONY: %-certs

%-certs: fetch-root-ca %/workload-cert-chain.pem k8s-root-cert.pem
	@echo "done"

%/workload-cert-chain.pem: k8s-root-cert.pem %/ca-cert.pem %/workload-cert.pem
	@echo "generating $@"
	@cat $^ > $@
	@echo "Intermediate and workload certs stored in $(dir $<)"
	@cp k8s-root-cert.pem $(dir $@)/root-cert.pem

%/workload-cert.pem: %/workload.csr
	@echo "generating $@"
	@openssl x509 -req -days $(WORKLOAD_DAYS) \
		-CA $(dir $<)/ca-cert.pem  -CAkey $(dir $<)/ca-key.pem -CAcreateserial\
		-extensions req_ext -extfile $(dir $<)/workload.conf \
		-in $< -out $@

%/workload.csr: L=$(dir $@)
%/workload.csr: %/key.pem %/workload.conf
	@echo "generating $@"
	@openssl req -new -config $(L)/workload.conf -key $< -out $@

%/key.pem:
	@echo "generating $@"
	@mkdir -p $(dir $@)
	@openssl genrsa -out $@ 4096