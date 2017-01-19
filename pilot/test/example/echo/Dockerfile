FROM ubuntu:14.04
ADD envoy_esp /usr/local/bin/envoy
ADD generic_service_config.json /etc/envoy/generic_service_config.json
ADD cmd /usr/local/bin/manager
RUN sudo chmod a+rw /etc/envoy
ENTRYPOINT ["/usr/local/bin/manager"]
