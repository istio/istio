# BASE_DISTRIBUTION is used to switch between the old base distribution and distroless base images
ARG BASE_DISTRIBUTION=default

# The following section is used as base image if BASE_DISTRIBUTION=default
FROM scratch as default
# obtained from debian ca-certs deb using fetch_cacerts.sh
ADD ca-certificates.tgz /

# The following section is used as base image if BASE_DISTRIBUTION=distroless
FROM gcr.io/distroless/static as distroless

# This will build the final image based on either default or distroless from above
FROM ${BASE_DISTRIBUTION}

# All containers need a /tmp directory
WORKDIR /tmp/
ADD istio_ca /usr/local/bin/istio_ca

ENTRYPOINT [ "/usr/local/bin/istio_ca", "--self-signed-ca" ]
