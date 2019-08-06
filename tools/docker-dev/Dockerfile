FROM docker:stable as docker

FROM ubuntu:xenial
ARG goversion=1.12.5
ARG user
ARG group
ARG uid=1000
ARG gid=1000

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# Install development packages.
RUN apt-get update && apt-get -qqy install --no-install-recommends \
autoconf=2.69-9 \
autotools-dev=20150820.1 \
build-essential=12.1ubuntu2 \
ca-certificates=20170717~16.04.2 \
curl=7.47.0-1ubuntu2.13 \
git=1:2.7.4-0ubuntu1.6 \
libtool=2.4.6-0.1 \
lsb-release=9.20160110ubuntu0.2 \
make=4.1-6 \
sudo=1.8.16-0ubuntu1.7 \
bash-completion=1:2.1-4.2ubuntu1.1 \
jq=1.5+dfsg-1ubuntu0.1 \
tmux=2.1-3build1 \
vim=2:7.4.1689-3ubuntu1.3 \
&& rm -rf /var/lib/apt/lists/*

# Create user and allow sudo without password.
RUN addgroup --quiet --gid $gid $group \
&& adduser --quiet --disabled-password --gecos ",,,," --uid $uid --ingroup $group $user \
&& echo "${user} ALL=(ALL:ALL) NOPASSWD: ALL" > /etc/sudoers.d/$user

# Install Docker CLI.
COPY --from=docker /usr/local/bin/docker /usr/local/bin/docker

# Fix the Docker socket access rights at login time to allow non-root access.
RUN echo "sudo chmod o+rw /var/run/docker.sock" >> /home/${user}/.bashrc

# Install Go.
RUN curl -s -Lo - https://dl.google.com/go/go${goversion}.linux-amd64.tar.gz | tar -C /usr/local -xzf - \
&& echo "# Go environment." >> /home/${user}/.bashrc \
&& echo "export GOROOT=/usr/local/go" >> /home/${user}/.bashrc \
&& echo "export GOPATH=~/go" >> /home/${user}/.bashrc \
&& echo "export PATH=\$GOROOT/bin:\$GOPATH/out/linux_amd64/release:\$GOPATH/bin:\$PATH" >> /home/${user}/.bashrc \
&& echo "export GO111MODULE=on" >> /home/${user}/.bashrc \
&& mkdir -p /home/${user}/go

# Install KIND 0.3.0.
# Cf. https://github.com/kubernetes-sigs/kind
RUN GO111MODULE="on" GOROOT=/usr/local/go GOPATH=/home/${user}/go /usr/local/go/bin/go get sigs.k8s.io/kind@v0.3.0

# Install Helm's latest release.
RUN curl -s -Lo - https://git.io/get_helm.sh | /bin/bash

# Install gcloud and kubectl.
RUN echo "deb http://packages.cloud.google.com/apt cloud-sdk-$(lsb_release -c -s) main" | tee -a /etc/apt/sources.list.d/google-cloud-sdk.list \
&& curl -s https://packages.cloud.google.com/apt/doc/apt-key.gpg | apt-key add - \
&& apt-get update && apt-get -qqy install --no-install-recommends \
google-cloud-sdk=257.0.0-0 \
kubectl=1.15.2-00 \
&& rm -rf /var/lib/apt/lists/*

# Install bash completion files.
RUN /home/${user}/go/bin/kind completion bash > /etc/bash_completion.d/kind \
&& /usr/local/bin/helm completion bash > /etc/bash_completion.d/helm \
&& /usr/bin/kubectl completion bash > /etc/bash_completion.d/kubectl \
&& curl -s -Lo - https://raw.githubusercontent.com/docker/cli/master/contrib/completion/bash/docker > /etc/bash_completion.d/docker

USER $user
WORKDIR /home/$user/go/src/istio.io/istio
ENTRYPOINT ["/bin/bash", "-c"]
