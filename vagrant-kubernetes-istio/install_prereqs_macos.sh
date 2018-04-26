echo "Install docker"
brew cask install docker

echo "Install virtualbox"
brew cask install virtualbox

echo "Install vagrant"
brew cask install vagrant

echo "Install kubectl"
wget -O https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl
chmod +x ./kubectl
sudo mv ./kubectl /usr/local/bin/kubectl
