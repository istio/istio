
```shell
# == Get repo setup
git clone git@github.com:istio/istio.git
cd istio
git checkout 1.26.4
git remote add rash git@github.com:ddl-r-abdulaziz/istio.git


# == Install tools
sudo apt install -y make tmux

sudo apt-get -y update
sudo apt-get install -y ca-certificates curl
sudo install -y -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc

echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo "${UBUNTU_CODENAME:-$VERSION_CODENAME}") stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update -y

sudo apt-get -y install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# == login to quay.io
sudo docker login -u='rabdulaziz' -p='<<get it from quay>>' quay.io

# == build
export HUB=quay.io/rabdulaziz 
export TAG=debug1
sudo make build
sudo make push 

# == copy
docker tag $HUB/pilot:$TAG quay.io/domino/istio-pilot:$TAG
docker push quay.io/domino/istio-pilot:$TAG
```

