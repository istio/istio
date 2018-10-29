# Concatenated after ../Dockerfile to create the tgz
FROM docker.io/fortio/fortio.build:v12 as stage
WORKDIR /stage
COPY --from=release /usr/bin/fortio usr/bin/fortio
COPY --from=release /usr/share/fortio usr/share/fortio
COPY docs/fortio.1 usr/share/man/man1/fortio.1
RUN mkdir /tgz
# Make sure the list here is both minimal and complete
# we could take all of * but that adds system directories to the tar
RUN tar cvf - usr/share/fortio/* usr/share/man/man1/fortio.1 usr/bin/fortio | gzip --best > /tgz/fortio-linux_x64-$(./usr/bin/fortio version -s).tgz
WORKDIR /tgz
COPY release/ffpm.sh /
RUN bash -x /ffpm.sh deb
RUN bash -x /ffpm.sh rpm
FROM scratch
COPY --from=stage /tgz/ /tgz/
