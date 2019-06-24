# BASE_DISTRIBUTION is used to switch between the old base distribution and distroless base images
ARG BASE_DISTRIBUTION=default

# The following section is used as base image if BASE_DISTRIBUTION=default
FROM scratch as default

# The following section is used as base image if BASE_DISTRIBUTION=distroless
FROM gcr.io/distroless/static as distroless

# This will build the final image based on either default or distroless from above
FROM ${BASE_DISTRIBUTION}

# All containers need a /tmp directory
WORKDIR /tmp/
ADD node_agent /usr/local/bin/node_agent

ENTRYPOINT [ "/usr/local/bin/node_agent"]
