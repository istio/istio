#!/bin/bash

echo Preparing linters
bin/preplinters.sh

echo Running linters
bin/runlinters.sh

echo Done running linters
