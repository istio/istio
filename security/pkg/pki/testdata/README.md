# Generating ECC certificate for unit test

In general, we prefer to use certs by running `security/tools/generate_cert/main.go`

## ECC root certificate

```
go run main.go -ec-sig-alg ECDSA -ca true
```

## ECC client certificate signed by root certificate

```
go run main.go -ec-sig-alg ECDSA -san watt -signer-cert ../../pkg/pki/testdata/ec-root-cert-new.pem -signer-priv ../../pkg/pki/testdata/ec-root-key-new.pem
```
