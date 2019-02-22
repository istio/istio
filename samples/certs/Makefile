.SUFFIXES: .csr .pem .conf
.PRECIOUS: %/ca-key.pem %/ca-cert.pem %/cert-chain.pem
#.PRECIOUS: %/cluster-ca.csr %/intermediate.conf

.DEFAULT_GOAL := help

#------------------------------------------------------------------------
# variables: root CA
ROOTCA_DAYS ?= 3650 					# 10 years
ROOTCA_SUBJ ?= "/O=Istio/CN=Istio Root CA"

#------------------------------------------------------------------------
# variables: intermediate CA (Citadel)
CITADEL_SERIAL ?= $(shell echo $$PPID) 	# certificate serial number (uses current PID)
CITADEL_DAYS ?= 730						# 2 years
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
	@openssl x509 -req -sha256 -days $(ROOTCA_DAYS) -signkey root-key.pem -in $< -out $@
	@rm $<

root-cert.csr: root-key.pem
	@echo "generating $@"
	@openssl req -new -sha256 -subj $(ROOTCA_SUBJ) -key $< -out $@ 

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
	@openssl x509 -req -days $(CITADEL_DAYS) -sha256 \
		-CA root-cert.pem -CAkey root-key.pem -set_serial $(CITADEL_SERIAL) \
		-extensions req_ext -extfile $(dir $<)/intermediate.conf \
		-in $< -out $@

%/cluster-ca.csr: L=$(dir $@)
%/cluster-ca.csr: %/ca-key.pem %/intermediate.conf
	@echo "generating $@"
	@openssl req -new -batch -sha256 $(UTF8) \
		-config $(L)/intermediate.conf \
		-key $< -out $@ 

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
	@echo "default_bits = 4096" >> $@
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
	@echo "[ req_dn ]" >> $@
	@echo "O = Istio" >> $@
	@echo "CN = Intermediate CA" >> $@
	@echo "L = $(L:/=)" >> $@
