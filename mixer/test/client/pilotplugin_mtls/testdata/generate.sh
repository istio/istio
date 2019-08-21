#!/bin/sh

openssl genrsa -out root.key 2048
openssl req -x509 -new -nodes -key root.key -sha256 -days 1825 -out root.cert

# generate mTLS cert for client as follows:
go run security/tools/generate_cert/main.go -host="spiffe://cluster.local/ns/default/sa/client" -signer-priv=mixer/test/client/pilotplugin_mtls/testdata/root.key -signer-cert=mixer/test/client/pilotplugin_mtls/testdata/root.cert --mode=signer
