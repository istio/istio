FROM alpine:3.7 as go_generate_dependency_builder
RUN apk add --no-cache build-base git

RUN apk update
RUN apk add --no-cache go>1.10
ENV GOPATH=/go \
    PATH=/go/bin/:$PATH
RUN go get -u -v \
    github.com/jteeuwen/go-bindata/... \
    github.com/maxbrunsfeld/counterfeiter

ENTRYPOINT ["counterfeiter"]

