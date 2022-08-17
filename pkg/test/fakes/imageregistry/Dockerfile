FROM scratch
ARG TARGETARCH
COPY main-${TARGETARCH:-amd64}  /registry
ENTRYPOINT ["/registry"]
