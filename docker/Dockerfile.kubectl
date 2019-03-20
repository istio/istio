FROM istionightly/base_debug
# Image for post install jobs

# This container should only contain kubectl.  Hard-coding to use Linux K8s 1.11.1 version.
ADD https://storage.googleapis.com/kubernetes-release/release/v1.11.1/bin/linux/amd64/kubectl /usr/bin/kubectl
RUN chmod +x /usr/bin/kubectl

# Ensure a kubectl compatible with the container OS was used.  This is a safety
# measure to make sure even if this Dockerfile is changed we don't pick up OSX kubectl.
RUN /usr/bin/kubectl --help
