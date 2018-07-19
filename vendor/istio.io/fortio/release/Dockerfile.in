# Concatenated after ../Dockerfile to create the tgz
FROM istio/fortio.build:v8 as stage
WORKDIR /stage
COPY --from=release /usr/local usr/local
RUN mkdir -p var/lib/istio/fortio
# So fortio ran as whoever can write there:
RUN chmod 1777 var/lib/istio/fortio # /tmp like perms
RUN mkdir /tgz
# Make sure the list here is both minimal and complete
# we could take all of * but that adds system directories to the tar
RUN tar cvf - var/lib/istio/fortio usr/local/lib/fortio/* usr/local/bin/fortio | gzip --best > /tgz/fortio-linux_x64-$(./usr/local/bin/fortio version -s).tgz
FROM scratch
COPY --from=stage /tgz/ /tgz/
