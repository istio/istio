FROM scratch
ARG TARGETARCH
COPY ./main-${TARGETARCH:-amd64}  /gce-metadata-server
EXPOSE 8080
CMD ["/gce-metadata-server"]
