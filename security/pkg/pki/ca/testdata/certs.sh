# Root
openssl genrsa -out root-key.pem 4096
openssl req -new -key root-key.pem -out root-cert.csr -sha256 <<EOF
US
California
Sunnyvale
Istio
Test
Root CA
test@istio.io


EOF
openssl x509 -req -days 3650 -in root-cert.csr -sha256 -signkey root-key.pem -out root-cert.pem

# Intermediate
openssl genrsa -out ca-key.pem 4096
openssl req -new -key ca-key.pem -out ca-cert.csr -config ca-cert.cfg -batch -sha256

openssl x509 -req -days 3650 -in ca-cert.csr -sha256 -CA root-cert.pem -CAkey root-key.pem -CAcreateserial -out ca-cert.pem -extensions v3_req -extfile ca-cert.cfg

cat root-cert.pem > cert-chain.pem
cat ca-cert.pem >> cert-chain.pem

rm *csr
rm *srl
