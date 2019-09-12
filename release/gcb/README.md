To create local release run local.sh file.
You should provide docker hub where local images can be pushed and tag for the release.
You also can provide cni branch, by default it is going to be the same as for istio/istio, so if your istio/istio branch is something like my_favorite_branch and not master or release most likely cni repo does not have this branch and script will fail trying to fetch repo.

How to run the script:
CNI_BRANCH=master TAG=best_tag_ever DOCKER_HUB=docker.io/awesome_hub bash local.sh

The script is going to do next steps.

1. Create manifest file with all repos(istio. cni, tools, proxy, api) SHAs. Make sure that repos on your local machine synced to correct SHAs.
1. Creates images, saves source code on your local machine.
1. Creates Istio images with new version tag and pushes to provided hub.
1. Creates charts with provided TAG and hub.

When you build the release make sure that your repos are in the state from which you want to build.

If you do not care about consistency bwetween your repos, you can specify VERIFY_CONSISTENCY=false


