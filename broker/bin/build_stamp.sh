#!/bin/bash

# This must be kept in sync with //pkg/version/version.go

# Setup build ID based on date and short SHA of latest commit.
echo "buildID $(date +%F)-$(git rev-parse --short HEAD)"

# Check for local changes
git diff-index --quiet HEAD --
if [[ $? == 0 ]];
then
  tree_status="Clean"
else
  tree_status="Modified"
fi
echo "buildStatus ${tree_status}"

# Check for version information
VERSION=$(git describe)
if [[ $? == 0 ]];
then
  echo "buildVersion ${VERSION}"
fi
