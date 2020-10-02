#!/bin/bash
for ((i=1;i<=100;i++));
do
   # your-unix-command-here
   # echo $i
   go test -run ./...  -count=1 >> flaky.txt
done
