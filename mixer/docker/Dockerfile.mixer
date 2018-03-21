FROM scratch

# obtained from debian ca-certs deb using fetch_cacerts.sh
ADD ca-certificates.tgz /
ADD mixs /usr/local/bin/

ENTRYPOINT ["/usr/local/bin/mixs", "server"]
CMD ["--configStoreURL=fs:///etc/opt/mixer/configroot","--configStoreURL=k8s://"]
