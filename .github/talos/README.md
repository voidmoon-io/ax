# Talos CI

Run `voidmoon-publish.yaml` manually on `voidmoon/talos-ci`. It tests the selected
workflow commit on the `voidmoon-ci` ARC scale set. The optional boolean `publish`
input publishes only after its test jobs pass; there is no separate source-ref input.
Actions are pinned by commit, Go follows go.mod, and runner/BuildKit images are
pinned by digest. GitHub caches are disabled; uploaded artifacts expire in seven days.

Runner image and activation instructions live in shart-cloud/talos-gitops at
`images/voidmoon-ci/README.md`. Runner-group access lives in voidmoon-io/github-admin.
Keep the workflow ref in that group synchronized when moving branches.
