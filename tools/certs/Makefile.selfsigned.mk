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

root-key.pem:
	@echo "generating $@"
	@openssl genrsa -out $@ 4096
#------------------------------------------------------------------------
##<name>-cacerts: generate self signed intermediate certificates for <name> and store them under <name> directory.
.PHONY: %-cacerts

%-cacerts: %/cert-chain.pem
	@echo "done"

%/cert-chain.pem: %/ca-cert.pem root-cert.pem
	@echo "generating $@"
	@cat $^ > $@
	@echo "Intermediate inputs stored in $(dir $<)"
	@cp root-cert.pem $(dir $<)


%/ca-cert.pem: %/cluster-ca.csr root-key.pem root-cert.pem
	@echo "generating $@"
	@openssl x509 -req -days $(INTERMEDIATE_DAYS) \
		-CA root-cert.pem -CAkey root-key.pem -CAcreateserial\
		-extensions req_ext -extfile $(dir $<)/intermediate.conf \
		-in $< -out $@

%/cluster-ca.csr: L=$(dir $@)
%/cluster-ca.csr: %/ca-key.pem %/intermediate.conf
	@echo "generating $@"
	@openssl req -new -config $(L)/intermediate.conf -key $< -out $@

%/ca-key.pem:
	@echo "generating $@"
	@mkdir -p $(dir $@)
	@openssl genrsa -out $@ 4096

#------------------------------------------------------------------------
##<namespace>-certs: generate intermediate certificates and sign certificates for a virtual machine connected to the namespace `<namespace> using serviceAccount `$SERVICE_ACCOUNT` using self signed root certs.
.PHONY: %-certs

%-certs: %/workload-cert-chain.pem root-cert.pem
	@echo "done"

%/workload-cert-chain.pem: root-cert.pem %/ca-cert.pem %/workload-cert.pem
	@echo "generating $@"
	@cat $^ > $@
	@echo "Intermediate and workload certs stored in $(dir $<)"
	@cp root-cert.pem $(dir $@)/root-cert.pem


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
