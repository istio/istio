This file helps note down some ways we have found useful for continuous local development using local e2e tests

1. Running a single test
   If you are debugging a single test, you can run it with `test.run` option in E2E_ARGS as follows:
   
   ```bash
   make e2e_mixer E2E_ARGS="--use_local_cluster --cluster_wide --test.run TestRedisQuota" HUB=localhost:5000 TAG=latest
   ```

1. Using `--skip_setup` and `--skip_cleanup` options in E2E tests
   If you are debugging a test, you can choose to skip the cleanup process, so that the test setup still remains and you
   can debug test easily. Also, if you are developing a new test and setup of istio system will remain same, you can use
   the option to skip setup too so that you can save time for setting up istio system in e2e test.
   
   Ex: Using --skip_cleanup option to debug test:
   
   ```bash
   make e2e_mixer E2E_ARGS="--use_local_cluster --cluster_wide --skip_cleanup --test.run TestRedisQuota" HUB=localhost:5000 TAG=latest
   ```
   
   Ex: Using --skip_cleanup and --skip_setup option for debugging/development of test:
   
   ```bash
   make e2e_mixer E2E_ARGS="--use_local_cluster --cluster_wide --skip_cleanup --skip_setup --test.run TestRedisQuota" HUB=localhost:5000 TAG=latest
   ```

1. Update just one istio component you are debugging.
   More often than not, you would be debugging one istio component say mixer, pilot, citadel etc. You can update a single 
   component when running the test using component specific HUB and TAG
   
   Ex: Consider a scenario where you are debugging a mixer test and just want to update mixer code when running the test again.
   1. First time, run test with option of --skip_cleanup
   
      ```bash
      make e2e_mixer E2E_ARGS="--use_local_cluster --cluster_wide --skip_cleanup --test.run TestRedisQuota" HUB=localhost:5000 TAG=latest
      ```
   
   1. Next update mixer code and build and push it to localregistry at localhost:5000 with a new TAG say latest1 
   (TAG is changed, to make sure we would be using the new mixer binary)
   
      ```bash
      make push.docker.mixer HUB=localhost:5000 TAG=latest1
      ```
     
   1. Use the newly uploaded mixer image in the test now using E2E test option --mixer_tag
   
      ```bash
      make e2e_mixer E2E_ARGS="--use_local_cluster --cluster_wide --skip_cleanup --mixer_tag latest1 --test.run TestRedisQuota" HUB=localhost:5000 TAG=latest
      ```
 
