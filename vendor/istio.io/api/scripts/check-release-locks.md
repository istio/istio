## How to Update

If you are making a proto change and "make release-lock-status" is
failing, you will need to update the status file. First keep in mind
that this check is to prevent backwards-incompatible changes against
previous releases.

1. First ensure the changes you are making are not breaking backwards
   compatibility with any of these releases.

1. Edit `scripts/check-release-locks.sh` and comment out the line `rm status`.

1. Run `make release-lock-status`.

1. Copy the file `status` over the file
    `releaselocks/release-<ver>/proto.lock.status`, corresponding to
   the release version.

1. `scripts/check-release-locks.sh` can be reverted.

1. Include `releaselocks/release-<ver>/proto.lock.status` in your PR
   and justification for the change.

Lock files should not be updated. These should be the `proto.lock`
result of running `protolock init` in HEAD of the corresponding
release branch.
