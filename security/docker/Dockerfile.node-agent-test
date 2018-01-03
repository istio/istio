FROM ubuntu:xenial

ADD node_agent /usr/local/bin/node_agent

COPY start_app.sh /usr/local/bin/start_app.sh
COPY app.js /usr/local/bin/app.js
COPY istio_ca.crt /usr/local/bin/istio_ca.crt
COPY node_agent.crt /usr/local/bin/node_agent.crt
COPY node_agent.key /usr/local/bin/node_agent.key

ENTRYPOINT [ "/bin/bash", "/usr/local/bin/start_app.sh" ]
