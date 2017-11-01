# Change this and ../WORKSPACE at the same time
FROM gcr.io/istio-testing/envoy:f8b4de9a80f1d6b500b3148e0e3364daffbb9dfc
ADD pilot-agent /usr/local/bin/pilot-agent

COPY envoy_pilot.json      /etc/istio/proxy/envoy_pilot.json
COPY envoy_pilot_auth.json /etc/istio/proxy/envoy_pilot_auth.json
COPY envoy_mixer.json      /etc/istio/proxy/envoy_mixer.json
COPY envoy_mixer_auth.json /etc/istio/proxy/envoy_mixer_auth.json

ENTRYPOINT ["/usr/local/bin/pilot-agent"]
