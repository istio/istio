#!/bin/bash

# Check if homebrew is installed
brew --help > /dev/null
if [ $? -ne 0 ]; then
    echo "Homebrew is not installed. Please go to https://docs.brew.sh/Installation to install Homebrew."
    exit 1
fi

echo "Update homebrew..."
brew update

echo "Checking curl"
curl --help > /dev/null
if [ $? -ne 0 ]; 
then
    echo "curl is not installed. Install it from homebrew."
    brew install curl
    if [ $? -ne 0 ]; 
    then
    	echo "Installation from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "curl exists."
fi

echo "Checking docker..."
docker --help > /dev/null
if [ $? -ne 0 ]; 
then
    echo "docker is not installed. Install it from homebrew cask."
    brew cask install docker
    if [ $? -ne 0 ]; 
    then
    	echo "Installation from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "docker exists. Please make sure to update it to latest version."
fi

echo "Checking vitualbox..."
virtualbox --help > /dev/null
if [ $? -ne 0 ]; 
then
    echo "virtualbox is not installed. Install it from homebrew cask."
    brew cask install virtualbox
    if [ $? -ne 0 ]; 
    then
    	echo "Installation from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "virtualbox is installed. Checking and upgrading if a newer version exists."
    brew cask reinstall --force virtualbox
    if [ $? -ne 0 ]; 
    then
    	echo "Installation from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
fi

echo "Checking vagrant..."
vagrant --help > /dev/null
if [ $? -ne 0 ]; 
then
    echo "vagrant is not installed. Install it from homebrew cask."
    brew cask install vagrant
    if [ $? -ne 0 ]; 
    then
    	echo "Installation from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "vagrant exists. Please make sure to update it to latest version."
    vagrant version
fi

echo "Checking kubectl..."
kubectl --help > /dev/null
if [ $? -ne 0 ]; 
then
    echo "kubectl is not installed. Installing the lastest stable release..."
    brew install kubectl
    if [ $? -ne 0 ]; 
    then
    	echo "Installation from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "kubectl exists. Please make sure to update it to latest version."
fi

echo "Prerequisite check and installation process finishes."
