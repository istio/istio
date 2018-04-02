# Self-signed certificates

    openssl genrsa -out cert.key 2048
    openssl req -new -x509 -sha256 -key cert.key -out cert.crt -days 3650

For the common name, please type the following FQDN:

    api.company.com
