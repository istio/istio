echo 'Generate key and cert for root CA.'
openssl req -newkey rsa:2048 -nodes -keyout root-key.pem -x509 -days 3650 -out root-cert.pem <<EOF
US
California
Sunnyvale
Istio
Test
Root CA
testrootca@istio.io


EOF

echo 'Generate private key for Istio CA.'
openssl genrsa -out ca-key.pem 2048

echo 'Generate CSR for Istio CA.'
openssl req -new -key ca-key.pem -out ca-cert.csr -config ca.cfg -batch -sha256

echo 'Sign the cert for Istio CA.'
openssl x509 -req -days 730 -in ca-cert.csr -sha256 -CA root-cert.pem -CAkey root-key.pem -CAcreateserial -out ca-cert.pem -extensions v3_req -extfile ca.cfg

rm *csr
rm *srl

echo 'Generate cert chain file.'
cp ca-cert.pem cert-chain.pem
