# Refresh istio debug image with the following commands.
#
# 	docker build . -t istio/debug:test
# 	docker push istio/debug:test

FROM ubuntu:xenial
RUN apt-get update && \
	    apt-get install -y \
	    curl \
	    iptables \
	    iproute2 \
	    iputils-ping \
	    dnsutils \
	    netcat \
	    tcpdump \
	    net-tools \
	    libc6-dbg gdb valgrind \
	    vim \
	    nano \
	    emacs \
	    wrk

ADD proxy-redirection-clear /usr/local/bin/proxy-redirection-clear
ADD proxy-redirection-restore /usr/local/bin/proxy-redirection-restore

CMD exec /bin/bash -c "trap : TERM INT; sleep infinity & wait"
