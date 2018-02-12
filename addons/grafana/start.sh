#!/bin/bash -e

/import_dashboard.sh &

# delegate to the original entry point
/run.sh
