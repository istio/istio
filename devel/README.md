# Developing on Istio

See also the [broker](https://github.com/istio/broker/blob/master/doc/dev/development.md) and [mixer](https://github.com/istio/mixer/blob/master/doc/dev/development.md)
development setup and guidelines

## Collection of scripts and notes for developing on Istio

For local development (building from source and running the major components) on Ubuntu/raw VM:

Assuming you did (once):
1. [Install bazel](https://bazel.build/versions/master/docs/install-ubuntu.html), note that as of this writing Bazel needs the `openjdk-8-jdk` VM (you might need to uninstall or get out of the way the `ibm-java80-jdk` that comes by default with GCE for instance)
2. Install required packages: `sudo apt-get install make openjdk-8-jdk libtool m4 autoconf uuid-dev`
3. Get the source trees
   ```bash
   mkdir github
   cd github/
   git clone https://github.com/istio/proxy.git
   git clone https://github.com/istio/mixer.git
   git clone https://github.com/istio/istio.git
   # if you want to do load tests:
   git clone https://github.com/wg/wrk.git
   ```
4. You can then use 
   - [update_all](update_all) : script to build from source 
   - [setup_run](setup_run)  : run locally
   - Also found in this directory: [rules.yaml](rules.yaml) : the version of  mixer/testdata/configroot/scopes/global/subjects/global/rules.yml that works locally
5. And run things like
   ```bash
   # Test the echo server:
   curl -v http://localhost:8080/
   # Test through the proxy:
   curl -v http://localhost:9090/echo
   # Add a rule locally (simply drop the file or exercise the API:)
   curl -v  http://localhost:9094/api/v1/scopes/global/subjects/foo.svc.cluster.local/rules --data-binary @quota.yaml -X PUT -H "Content-Type: application/yaml"
   # Test under some load:
   wrk http://localhost:9090/echo
   
   ```
   Note that this is done for you by [steup_run](setup_run) but to use the correct go environment:
   ```bash
   cd mixer/
   source bin/use_bazel_go.sh 
   ```


## MacOs tips
 
Get GitHub desktop https://desktop.github.com/
 
If you want to make changes to the [website](https://github.com/istio/istio.github.io), and want to run jekyll locally and natively, without docker):

You will need a newer ruby than the default: get and install rvm https://rvm.io/
 
Then rvm install ruby-2.1 (or later) rvm use ruby-2.1  then `gem install jekyll bundler` then `bundle install` and then finally you can run successfully `bundle exec jekyll serve` in the directory you cloned the iostio doc repo. To avoid `GitHub Metadata: No GitHub API authentication could be found. Some fields may be missing or have incorrect data.` errors you need to set a public repo access token at https://github.com/settings/tokens and export it in `JEKYLL_GITHUB_TOKEN` env var (in your `.bash_profile`) - then http://127.0.0.1:4000/docs/ will work and auto update when pulling.
