FROM golang:1.24 AS builder
WORKDIR /app
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /race-client .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /race-client /race-client
ENTRYPOINT ["/race-client"]
