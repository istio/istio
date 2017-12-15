FROM scratch
ADD mixs /usr/local/bin/
# obtained from debian ca-certs deb using fetch_cacerts.sh
ADD ca-certificates.tgz /
WORKDIR /tmp/
ENTRYPOINT ["/usr/local/bin/mixs", "server"]
CMD ["--configStoreURL=fs:///etc/opt/mixer/configroot","--configStore2URL=k8s://","--logtostderr","--v=2"]
