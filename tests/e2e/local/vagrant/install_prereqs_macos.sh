#!/bin/bash

source ../common_macos.sh

check_homebrew

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

install_docker

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

install_kubectl

echo "Prerequisite check and installation process finishes."
