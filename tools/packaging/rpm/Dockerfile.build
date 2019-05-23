FROM centos:7

RUN yum upgrade -y && \
    yum install -y epel-release centos-release-scl && \
    yum install -y fedpkg golang sudo make which cmake3 \
                   automake autoconf autogen libtool \
                   devtoolset-6-gcc devtoolset-6-gcc-c++ \
                   devtoolset-6-libatomic-devel ninja-build && \
    yum clean all

RUN curl -o /root/bazel-installer.sh -L http://github.com/bazelbuild/bazel/releases/download/0.22.0/bazel-0.22.0-installer-linux-x86_64.sh && \
    chmod +x /root/bazel-installer.sh && \
    /root/bazel-installer.sh

RUN ln -s /usr/bin/cmake3 /usr/bin/cmake && \
    ln -s /usr/bin/ninja-build /usr/bin/ninja
