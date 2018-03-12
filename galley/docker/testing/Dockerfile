FROM ubuntu:xenial

ADD init.sh /
RUN chmod +x /init.sh

ADD bin/etcd /bin/etcd
ADD bin/kubectl /bin/kubectl
ADD bin/kube-apiserver /bin/kube-apiserver

# All the temporary data will be stored in /app.
RUN mkdir /app
VOLUME /app

# Expose the API Server at 8080
EXPOSE 8080

CMD ["/init.sh"]