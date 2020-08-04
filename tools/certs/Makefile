.SUFFIXES: .csr .pem .conf
.PRECIOUS: %/ca-key.pem %/ca-cert.pem %/cert-chain.pem
.PRECIOUS: %/selfSigned-ca-key.pem %/selfSigned-ca-cert.pem %/selfSigned-ca-cert-chain.pem
.PRECIOUS: %/selfSigned-workload-cert-chain.pem %/selfSigned-workload-cert.pem
.PRECIOUS: %/workload-cert.pem %/key.pem %/workload-cert-chain.pem
.SECONDARY: root-cert.csr root-ca.conf %/cluster-ca.csr %/intermediate.conf

.DEFAULT_GOAL := help

#------------------------------------------------------------------------
# variables: root CA
ROOTCA_DAYS ?= 3650
ROOTCA_KEYSZ ?= 4096
ROOTCA_ORG ?= Istio
ROOTCA_CN ?= Root CA
KUBECONFIG ?= $(HOME)/.kube/config
ISTIO_NAMESPACE ?= istio-system
# Additional variables are defined in root-ca.conf target below.

#------------------------------------------------------------------------
# variables: intermediate CA
INTERMEDIATE_DAYS ?= 730
INTERMEDIATE_KEYSZ ?= 4096
INTERMEDIATE_ORG ?= Istio
INTERMEDIATE_CN ?= Intermediate CA
INTERMEDIATE_SAN_DNS ?= istiod.istio-system.svc
# Additional variables are defined in %/intermediate.conf target below.

#------------------------------------------------------------------------
# variables: workload certs: eg VM
WORKLOAD_DAYS ?= 1
SERVICE_ACCOUNT ?= default
WORKLOAD_CN ?= Workload

#------------------------------------------------------------------------
# variables: files to clean
FILES_TO_CLEAN+=k8s-root-cert.pem \
                 k8s-root-cert.srl  \
                 k8s-root-key.pem root-ca.conf root-cert.csr root-cert.pem root-cert.srl root-key.pem
#------------------------------------------------------------------------
# clean
.PHONY: clean

clean: ## Cleans all the intermediate files and folders previously generated.
	@rm -f $(FILES_TO_CLEAN)
#------------------------------------------------------------------------
##help:		print this help message
.PHONY: help

help: Makefile
	@sed -n 's/^##//p' $<

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
	@res=$(shell kubectl get secret istio-ca-secret  -n  $(ISTIO-NAMESPACE) >/dev/null 2>&1;  echo $$?)
ifeq ($(res), 1)
	@kubectl get secret cacerts   -n  $(ISTIO_NAMESPACE) -o "jsonpath={.data['ca-cert\.pem']}" | base64 -d > $(cluster)/k8s-root-cert.pem
	@kubectl get secret cacerts  -n  $(ISTIO_NAMESPACE) -o "jsonpath={.data['ca-key\.pem']}" | base64 -d > $(cluster)/k8s-root-key.pem
else
	@kubectl get secret istio-ca-secret   -n  $(ISTIO_NAMESPACE) -o "jsonpath={.data['ca-cert\.pem']}" | base64 -d > $(cluster)/k8s-root-cert.pem
	@kubectl get secret istio-ca-secret   -n  $(ISTIO_NAMESPACE) -o "jsonpath={.data['ca-key\.pem']}" | base64 -d > $(cluster)/k8s-root-key.pem
endif

k8s-root-cert.pem:
	@cat $(cluster)/k8s-root-cert.pem > $@

k8s-root-key.pem:
	@cat $(cluster)/k8s-root-key.pem > $@
#------------------------------------------------------------------------
##root-ca:	generate root CA files (key and certifcate) in current directory.
.PHONY: root-ca

root-ca: root-key.pem root-cert.pem

root-cert.pem: root-cert.csr root-key.pem
	@echo "generating $@"
	@openssl x509 -req -days $(ROOTCA_DAYS) -signkey root-key.pem \
		-extensions req_ext -extfile root-ca.conf \
		-in $< -out $@

root-cert.csr: root-key.pem root-ca.conf
	@echo "generating $@"
	@openssl req -new -key $< -config root-ca.conf -out $@ 

root-ca.conf: 
	@echo "[ req ]" > $@
	@echo "encrypt_key = no" >> $@
	@echo "prompt = no" >> $@
	@echo "utf8 = yes" >> $@
	@echo "default_md = sha256" >> $@
	@echo "default_bits = $(ROOTCA_KEYSZ)" >> $@
	@echo "req_extensions = req_ext" >> $@
	@echo "x509_extensions = req_ext" >> $@
	@echo "distinguished_name = req_dn" >> $@
	@echo "[ req_ext ]" >> $@
	@echo "subjectKeyIdentifier = hash" >> $@
	@echo "basicConstraints = critical, CA:true" >> $@
	@echo "keyUsage = critical, digitalSignature, nonRepudiation, keyEncipherment, keyCertSign" >> $@
	@echo "[ req_dn ]" >> $@
	@echo "O = $(ROOTCA_ORG)" >> $@
	@echo "CN = $(ROOTCA_CN)" >> $@

root-key.pem:
	@echo "generating $@"
	@openssl genrsa -out $@ 4096


#------------------------------------------------------------------------
##<name>-cacerts-k8s: generate intermediate certificates for a cluster or VM with <name> signed with istio root cert from the specified k8s cluster and store them under <name> directory
.PHONY: %-cacerts-k8s

%-cacerts-k8s: %/cert-chain.pem
	@make clean
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
##<name>-cacerts-selfSigned: generate self signed intermediate certificates for <name> and store them under <name> directory.
.PHONY: %-cacerts-selfSigned

%-cacerts-selfSigned: %/selfSigned-ca-cert-chain.pem root-cert.pem
	@make clean
	@echo "done"

%/selfSigned-ca-cert-chain.pem: %/selfSigned-ca-cert.pem root-cert.pem
	@echo "generating $@"
	@cat $^ > $@
	@echo "Intermediate inputs stored in $(dir $<)"
	@cp root-cert.pem $(dir $<)


%/selfSigned-ca-cert.pem: %/selfSigned-cluster-ca.csr root-key.pem root-cert.pem
	@echo "generating $@"
	@openssl x509 -req -days $(INTERMEDIATE_DAYS) \
		-CA root-cert.pem -CAkey root-key.pem -CAcreateserial\
		-extensions req_ext -extfile $(dir $<)/intermediate.conf \
		-in $< -out $@

%/selfSigned-cluster-ca.csr: L=$(dir $@)
%/selfSigned-cluster-ca.csr: %/selfSigned-ca-key.pem %/intermediate.conf
	@echo "generating $@"
	@openssl req -new -config $(L)/intermediate.conf -key $< -out $@ 

%/selfSigned-ca-key.pem:
	@echo "generating $@"
	@mkdir -p $(dir $@)
	@openssl genrsa -out $@ 4096

%/intermediate.conf: L=$(dir $@)
%/intermediate.conf:
	@echo "[ req ]" > $@
	@echo "encrypt_key = no" >> $@
	@echo "prompt = no" >> $@
	@echo "utf8 = yes" >> $@
	@echo "default_md = sha256" >> $@
	@echo "default_bits = $(INTERMEDIATE_KEYSZ)" >> $@
	@echo "req_extensions = req_ext" >> $@
	@echo "x509_extensions = req_ext" >> $@
	@echo "distinguished_name = req_dn" >> $@
	@echo "[ req_ext ]" >> $@
	@echo "subjectKeyIdentifier = hash" >> $@
	@echo "basicConstraints = critical, CA:true, pathlen:0" >> $@
	@echo "keyUsage = critical, digitalSignature, nonRepudiation, keyEncipherment, keyCertSign" >> $@
	@echo "subjectAltName=@san" >> $@
	@echo "[ san ]" >> $@
	@echo "DNS.1 = $(INTERMEDIATE_SAN_DNS)" >> $@
	@echo "[ req_dn ]" >> $@
	@echo "O = $(INTERMEDIATE_ORG)" >> $@
	@echo "CN = $(INTERMEDIATE_CN)" >> $@
	@echo "L = $(L:/=)" >> $@
#------------------------------------------------------------------------
##<namespace>-certs-selfSigned: generate intermediate certificates and sign certificates for a virtual machine connected to the namespace `<namespace> using serviceAccount `$SERVICE_ACCOUNT` using self signed root certs.
.PHONY: %-certs-selfSigned

%-certs-selfSigned: %/selfSigned-workload-cert-chain.pem root-cert.pem
	@make clean
	@echo "done"

%/selfSigned-workload-cert-chain.pem: root-cert.pem %/selfSigned-ca-cert.pem %/selfSigned-workload-cert.pem	
	@echo "generating $@"
	@cat $^ > $@
	@echo "Intermediate and workload certs stored in $(dir $<)"
	@cp root-cert.pem $(dir $@)/root-cert.pem


%/selfSigned-workload-cert.pem: %/workload.csr
	@echo "generating $@"
	@openssl x509 -req -days $(WORKLOAD_DAYS) \
		-CA $(dir $<)/selfSigned-ca-cert.pem  -CAkey $(dir $<)/selfSigned-ca-key.pem -CAcreateserial\
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

%/workload.conf: L=$(dir $@)
%/workload.conf:
	@echo "[ req ]" > $@
	@echo "encrypt_key = no" >> $@
	@echo "prompt = no" >> $@
	@echo "utf8 = yes" >> $@
	@echo "default_md = sha256" >> $@
	@echo "default_bits = $(INTERMEDIATE_KEYSZ)" >> $@
	@echo "req_extensions = req_ext" >> $@
	@echo "x509_extensions = req_ext" >> $@
	@echo "distinguished_name = req_dn" >> $@
	@echo "[ req_ext ]" >> $@
	@echo "subjectKeyIdentifier = hash" >> $@
	@echo "basicConstraints = critical, CA:false" >> $@
	@echo "keyUsage = digitalSignature, keyEncipherment" >> $@
	@echo "extendedKeyUsage = serverAuth, clientAuth" >> $@
	@echo "subjectAltName=@san" >> $@
	@echo "[ san ]" >> $@
	@echo "URI.1 = spiffe://cluster.local/ns/$(L)sa/$(SERVICE_ACCOUNT)" >> $@
	@echo "DNS.1 = spiffe://cluster.local/ns/$(L)sa/$(SERVICE_ACCOUNT)" >> $@
	@echo "[ req_dn ]" >> $@
	@echo "O = $(INTERMEDIATE_ORG)" >> $@
	@echo "CN = $(WORKLOAD_CN)" >> $@
	@echo "L = $(L:/=)" >> $@

#------------------------------------------------------------------------
##<namespace>-certs-k8s: generate intermediate certificates and sign certificates for a virtual machine connected to the namespace `<namespace> using serviceAccount `$SERVICE_ACCOUNT` using root cert from k8s cluster.
.PHONY: %-certs-k8s

%-certs-k8s: %/workload-cert-chain.pem k8s-root-cert.pem
	@make clean
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

