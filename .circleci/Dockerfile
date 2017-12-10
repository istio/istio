# Use the JDK image to avoid installing it again.
FROM    circleci/openjdk:latest

# this will install the latest version of bazel - unfortunately it won't
# work, since they break backward compat on every single release.
# Proxy is currently requiring 0.7.
#RUN \
#    sudo sh -c 'echo "deb [arch=amd64] http://storage.googleapis.com/bazel-apt stable jdk1.8" > /etc/apt/sources.list.d/bazel.list ' && \
#    curl https://storage.googleapis.com/bazel-apt/doc/apt-key.pub.gpg | sudo apt-key add -


RUN sudo apt-get update && \
    sudo apt-get -y install \
    wget software-properties-common make cmake python python-pip \
    zlib1g-dev bash-completion bc libtool automake zip time g++-6 gcc-6 \
    rsync

# ~100M, depends on g++, zlib1g-dev, bash-completions
RUN curl -Lo /tmp/bazel.deb https://github.com/bazelbuild/bazel/releases/download/0.7.0/bazel_0.7.0-linux-x86_64.deb && \
    sudo dpkg -i /tmp/bazel.deb && rm /tmp/bazel.deb


# Instead of "apt-get -y install  golang"
RUN cd /tmp && \
    wget https://redirector.gvt1.com/edgedl/go/go1.9.2.linux-amd64.tar.gz && \
    sudo rm -rf /usr/local/go && \
    sudo tar -C /usr/local -xzf go1.9.2.linux-amd64.tar.gz && \
    sudo chown -R circleci /usr/local/go && \
    sudo ln -s /usr/local/go/bin/go /usr/local/bin

RUN bazel version

# For circleci unit test integration, "go test -v 2>&1 | go-junit-report > report.xml"
RUN go get -u github.com/jstemmer/go-junit-report
