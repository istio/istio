# Image for building and running Kind
# Kind will be used in a CI/CD system
# Already present: kubectl, kubeadm, ubuntu, docker
FROM  kindest/node:latest

# Environment variables used in the build.
ENV     GOPATH=/go
ENV     PATH=/go/bin:/go/out/linux_amd64:$PATH

# Need to have docker installed
#RUN curl -s -L https://get.docker.io/ | sh -x

ADD kind /usr/local/bin
ADD helm /usr/local/bin


