echo Building server
go build -o server main/main.go
echo Building container
docker build -t gcr.io/istio-testing/proxy2:latest .
