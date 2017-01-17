FROM ubuntu:14.04
ADD envoy_esp /usr/local/bin/envoy
ADD generic_service_config.json /etc/envoy/generic_service_config.json
ADD cmd /cmd
ENTRYPOINT ["/cmd"]
