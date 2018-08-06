#!/bin/bash

# Check if homebrew is installed
if ! brew --help > /dev/null; then
    echo "Homebrew is not installed. Please go to https://docs.brew.sh/Installation to install Homebrew."
    exit 1
fi

echo "Update homebrew..."
brew update

echo "Checking curl"
if ! curl --help > /dev/null; then
    echo "curl is not installed. Install it from homebrew."
    if ! brew install curl; then
    	echo "Installation from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "curl exists."
fi

echo "Checking docker..."
if ! docker --help > /dev/null; then
    echo "docker is not installed. Install it from homebrew cask."
    if ! brew cask install docker; then
    	echo "Installation from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "docker exists. Please make sure to update it to latest version."
fi

echo "Checking vitualbox..."
if ! virtualbox --help > /dev/null; then
    echo "virtualbox is not installed. Install it from homebrew cask."
    if ! brew cask install virtualbox; then
    	echo "Installation from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "virtualbox is installed. Checking and upgrading if a newer version exists."
    if ! brew cask reinstall --force virtualbox; then
    	echo "Installation from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
fi

echo "Checking vagrant..."
if ! vagrant --help > /dev/null; then
    echo "vagrant is not installed. Install it from homebrew cask."
    if ! brew cask install vagrant; then
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
if ! kubectl --help > /dev/null; then
    echo "kubectl is not installed. Installing the lastest stable release..."
    if ! brew install kubectl; then
    	echo "Installation from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "kubectl exists. Please make sure to update it to latest version."
fi

echo "Prerequisite check and installation process finishes."
