.PHONY: build docker docker.push

build: main.go
	CGO_ENABLED=0 GOOS=linux go build -o solarwindsadapter .
docker: solarwindsadapter
	docker build . -t your-repo/solarwindsadapter:0.1.0
docker.push: 
	docker push your-repo/solarwindsadapter:0.1.0
