.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o dogstatsdadapter .
docker: dogstatsdadapter
	docker build . -t your-repo/dogstatsdadapter:0.1.0
docker.push: 
	docker push your-repo/dogstatsdadapter:0.1.0
