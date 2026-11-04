#!/usr/bin/env bash

# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Runs every root-gated test package under sudo. A package is root-gated when
# its tests import internal/roottest (the tests self-skip unless run as root),
# so adding privileged tests anywhere picks them up here with no CI change.
# Extra arguments are passed through to `go test` (e.g. -v, -run).

set -o errexit -o nounset -o pipefail

# This pod is ephemeral; never use the Talos host's Docker socket or kubeconfig.
sudo -n true
test -c /dev/kvm
sudo dockerd --host=unix:///var/run/docker.sock --group=docker \
  --ipv6 --fixed-cidr-v6=fd00:bee:1::/64 2>&1 | tee "${RUNNER_TEMP}/dockerd.log" &
for _attempt in {1..60}; do
  if docker info >/dev/null 2>&1; then
    docker run --rm --device /dev/kvm busybox:1.37.0@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec test -c /dev/kvm
    exit 0
  fi
  sleep 1
done
cat "${RUNNER_TEMP}/dockerd.log"
exit 1
