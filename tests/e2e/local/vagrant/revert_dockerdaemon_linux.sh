#!/bin/bash

# Cleanup Setup on host to talk to insecure registry on VM.
sudo cp /lib/systemd/system/docker.service_old /lib/systemd/system/docker.service
sudo rm -rf /lib/systemd/system/docker.service_old
sudo systemctl daemon-reload
sudo systemctl restart docker
