#! /bin/bash
# Extract fortio's help and rewrap it to 80 cols
# TODO: do like fmt does to keep leading identation
fortio help | expand | fold -s | sed -e "s/ $//"
