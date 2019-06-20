FROM docker:latest as docker

FROM ubuntu:latest
ARG goversion=1.12.5
ARG user
ARG group
ARG uid=1000
ARG gid=1000

# Install development packages.
RUN apt-get update && apt-get -qqy upgrade && apt-get -qqy install \
autoconf \
autotools-dev \
build-essential \
curl \
git \
libtool \
lsb-release \
make \
sudo \
bash-completion \
jq \
tmux \
vim \
&& rm -rf /var/lib/apt/lists/*

# Create user and allow sudo without password.
RUN addgroup --quiet --gid $gid $group \
&& adduser --quiet --disabled-password --gecos ',,,,' --uid $uid --ingroup $group $user \
&& echo "${user} ALL=(ALL:ALL) NOPASSWD: ALL" > /etc/sudoers.d/$user

# Install Docker CLI.
COPY --from=docker /usr/local/bin/docker /usr/local/bin/docker

# Fix the Docker socket access rights at login time to allow non-root access.
RUN echo 'sudo chmod o+rw /var/run/docker.sock' >> /home/${user}/.bashrc

# Install Go.
RUN curl -s -Lo - https://dl.google.com/go/go${goversion}.linux-amd64.tar.gz | tar -C /usr/local -xzf - \
&& echo '# Go environment.' >> /home/${user}/.bashrc \
&& echo 'export GOROOT=/usr/local/go' >> /home/${user}/.bashrc \
&& echo 'export GOPATH=~/go' >> /home/${user}/.bashrc \
&& echo 'export PATH=$GOROOT/bin:$GOPATH/out/linux_amd64/release:$GOPATH/bin:$PATH' >> /home/${user}/.bashrc \
&& echo 'export GO111MODULE=on' >> /home/${user}/.bashrc \
&& mkdir -p /home/${user}/go

# Install KIND 0.3.0.
# Cf. https://github.com/kubernetes-sigs/kind
RUN GO111MODULE="on" GOROOT=/usr/local/go GOPATH=/home/${user}/go /usr/local/go/bin/go get sigs.k8s.io/kind@v0.3.0

# Install Helm's latest release.
RUN curl -s -Lo - https://git.io/get_helm.sh | /bin/bash

# Install gcloud and kubectl.
RUN echo "deb http://packages.cloud.google.com/apt cloud-sdk-$(lsb_release -c -s) main" | tee -a /etc/apt/sources.list.d/google-cloud-sdk.list \
&& curl -s https://packages.cloud.google.com/apt/doc/apt-key.gpg | apt-key add - \
&& apt-get update && apt-get -qqy install \
google-cloud-sdk \
kubectl \
&& rm -rf /var/lib/apt/lists/*

# Install bash completion files.
RUN /home/${user}/go/bin/kind completion bash > /etc/bash_completion.d/kind \
&& /usr/local/bin/helm completion bash > /etc/bash_completion.d/helm \
&& /usr/bin/kubectl completion bash > /etc/bash_completion.d/kubectl \
&& curl -s -Lo - https://raw.githubusercontent.com/docker/cli/master/contrib/completion/bash/docker > /etc/bash_completion.d/docker

USER $user
WORKDIR /home/$user/go/src/istio.io/istio
ENTRYPOINT ["/bin/bash", "-c"]
