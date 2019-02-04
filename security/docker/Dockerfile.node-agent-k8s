FROM ubuntu:16.04
ADD node_agent_k8s /
RUN apt-get update && apt-get install -y ca-certificates
RUN chmod 755 /node_agent_k8s
ENTRYPOINT ["/node_agent_k8s"]
