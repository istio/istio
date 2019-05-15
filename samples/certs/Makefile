.SUFFIXES: .csr .pem .conf
.PRECIOUS: %/ca-key.pem %/ca-cert.pem %/cert-chain.pem
.PRECIOUS: root-cert.csr root-ca.conf %/cluster-ca.csr %/intermediate.conf
.SECONDARY: root-cert.csr root-ca.conf %/cluster-ca.csr %/intermediate.conf

.DEFAULT_GOAL := help

#------------------------------------------------------------------------
# variables: root CA
ROOTCA_DAYS ?= 3650
ROOTCA_KEYSZ ?= 4096
ROOTCA_ORG ?= Istio
ROOTCA_CN ?= Root CA
# Additional variables are defined in root-ca.conf target below.

#------------------------------------------------------------------------
# variables: intermediate CA (Citadel)
CITADEL_SERIAL ?= $(shell echo $$PPID) 	# certificate serial number (uses current PID)
CITADEL_DAYS ?= 730
CITADEL_KEYSZ ?= 4096
CITADEL_ORG ?= Istio
CITADEL_CN ?= Intermediate CA
CITADEL_SAN_DNS ?= localhost
# Additional variables are defined in %/intermediate.conf target below.

#------------------------------------------------------------------------
##help:		print this help message
.PHONY: help

help: Makefile
	@sed -n 's/^##//p' $<

#------------------------------------------------------------------------
##root-ca:	generate root CA files (key and certifcate) in current directory
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
##<name>-certs:	generate Citadel certificates for <name>. Includes all PEM files needed.
.PHONY: %-certs

%-certs: %/cert-chain.pem root-cert.pem
	@echo "Citadel inputs stored in $(dir $<)"
	@cp root-cert.pem $(dir $<)

%/cert-chain.pem: %/ca-cert.pem root-cert.pem
	@echo "generating $@"
	@cat $^ > $@

%/ca-cert.pem: %/cluster-ca.csr root-key.pem root-cert.pem
	@echo "generating $@"
	@openssl x509 -req -days $(CITADEL_DAYS) \
		-CA root-cert.pem -CAkey root-key.pem -set_serial $(CITADEL_SERIAL) \
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

%/intermediate.conf: L=$(dir $@)
%/intermediate.conf:
	@echo "[ req ]" > $@
	@echo "encrypt_key = no" >> $@
	@echo "prompt = no" >> $@
	@echo "utf8 = yes" >> $@
	@echo "default_md = sha256" >> $@
	@echo "default_bits = $(CITADEL_KEYSZ)" >> $@
	@echo "req_extensions = req_ext" >> $@
	@echo "x509_extensions = req_ext" >> $@
	@echo "distinguished_name = req_dn" >> $@
	@echo "[ req_ext ]" >> $@
	@echo "subjectKeyIdentifier = hash" >> $@
	@echo "basicConstraints = critical, CA:true, pathlen:0" >> $@
	@echo "keyUsage = critical, digitalSignature, nonRepudiation, keyEncipherment, keyCertSign" >> $@
	@echo "subjectAltName=@san" >> $@
	@echo "[ san ]" >> $@
	@echo "URI.1 = spiffe://cluster.local/ns/istio-system/sa/citadel" >> $@
	@echo "URI.2 = spiffe://$(L:/=)/ns/istio-system/sa/citadel" >> $@
	@echo "DNS.1 = $(CITADEL_SAN_DNS)" >> $@
	@echo "[ req_dn ]" >> $@
	@echo "O = $(CITADEL_ORG)" >> $@
	@echo "CN = $(CITADEL_CN)" >> $@
	@echo "L = $(L:/=)" >> $@
