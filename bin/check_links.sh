#!/bin/bash
awesome_bot --skip-save-results --allow_ssl --allow-timeout --allow-dupe --allow-redirect --white-list https://github.com/$GITHUB_USER/istio *.md **/*.md
