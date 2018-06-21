#!/bin/bash

phub='{{\([ $]*\.Values\.global\)\.hub }}'
ptag='{{[ $]*\.Values\.global\.tag }}'

hub='{{\1.hub }}{{\1.hub_img_delim }}'
tag='{{\1.img_tag_delim }}{{\1.tag }}'


set -x
git grep -l "${phub}/[^:]\+:${ptag}" | while read f; do
    sed -i '' "s,${phub}/\\([^:][^:]*\\):${ptag},${hub}\\2${tag}," $f;
done
