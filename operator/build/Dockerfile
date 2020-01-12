FROM ubuntu:xenial as base_os_context

ENV OPERATOR=/usr/local/bin/istio-operator \
    USER_UID=1001 \
    USER_NAME=istio-operator

# install operator binary
COPY istio-operator /usr/local/bin/.
COPY bin /usr/local/bin
RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
