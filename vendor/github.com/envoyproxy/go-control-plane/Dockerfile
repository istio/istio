# Integration test docker file
FROM envoyproxy/envoy:latest
ADD sample /sample
ADD build/integration.sh build/integration.sh
ADD bin/test-linux /bin/test
ENTRYPOINT ["build/integration.sh"]
