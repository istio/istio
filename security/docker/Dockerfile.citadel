FROM scratch

# obtained from debian ca-certs deb using fetch_cacerts.sh
ADD ca-certificates.tgz /
# All containers need a /tmp directory
WORKDIR /tmp/
ADD istio_ca /usr/local/bin/istio_ca

ENTRYPOINT [ "/usr/local/bin/istio_ca", "--self-signed-ca" ]
