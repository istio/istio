#!/bin/bash
go run ./istioctl/cmd/istioctl manifest apply -y -f iop.yaml
kubectl create namespace prometheus
helm3 upgrade --install prometheus stable/prometheus --namespace prometheus --set alertmanager.enabled=false --set pushgateway.enabled=false --set server.persistentVolume.enabled=false --set server.global.scrape_interval=15s
