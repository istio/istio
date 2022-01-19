# SAN Certificate Creation with OpenSSL

### Creating CA

```
openssl genrsa -out ca.key 2048

openssl req -new -x509 -days 3650 -key ca.key -subj "/C=US/ST=AZ/O=Acme, Inc./CN=Acme Root CA" -out ca.crt
```

### Creating Server Certificate
```
openssl req -newkey rsa:2048 -nodes -keyout server.key -subj "/C=US/ST=AZ/O=Acme, Inc./CN=*.com" -out server.csr
```

### Signing Certificate with CA
```
openssl x509 -req -extfile <(printf "subjectAltName=DNS:jwt-server,DNS:localhost") -days 3650 -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt
```