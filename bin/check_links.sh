#!/bin/bash

# Awesome bot is a tool that checks links in markdown files.
# Install it with:
#
#   gem install awesome_bot

awesome_bot --skip-save-results --allow_ssl --allow-timeout --allow-dupe --allow-redirect --white-list https://github.com/$GITHUB_USER/istio ./*.md ./**/*.md
