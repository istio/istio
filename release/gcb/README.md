To create local release run local.sh
You should provide docker hub where local images can be pushed and version of the release.
It is going to perform next steps.

1. Create manifest file with all repos SHAs. Make sure that repos on your local machine synced to correct SHA.
2. Creates images, saves source code on your local machine.
// Work in Progress
3. Creates Istio images with new version tag and pushes to hub.
4. Updates charts with provided version.

When you build the release make sure that your repos are in the state from which you want to build.

If you do not care about consistency bwetween your repos, you can specify CB_VERIFY_CONSISTENCY=false


