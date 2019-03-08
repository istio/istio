.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o memquotaadapter .
docker: memquotaadapter
	docker build . -t your-repo/memquotaadapter:0.1.0
docker.push: 
	docker push your-repo/memquotaadapter:0.1.0
