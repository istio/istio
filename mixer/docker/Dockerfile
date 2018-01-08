ARG ISTIO_BIN=.
FROM scratch

# obtained from debian ca-certs deb using fetch_cacerts.sh
ADD ca-certificates.tgz /
ADD ${ISTIO_BIN}/mixs /usr/local/bin/

ENTRYPOINT ["/usr/local/bin/mixs", "server"]
CMD ["--configStoreURL=fs:///etc/opt/mixer/configroot","--configStore2URL=k8s://","--logtostderr","--v=2"]
