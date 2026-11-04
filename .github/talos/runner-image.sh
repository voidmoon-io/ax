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

set -o errexit -o nounset -o pipefail
image="ghcr.io/voidmoon-io/ax/ax-task-runner:${TAG}"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
  -o bin/linux_amd64/ax-task-runner ./cmd/ax-task-runner
# Upstream .dockerignore excludes bin/, which Dockerfile.task-runner
# COPYs from (their Makefile target fails the same way).
sed -i '/^bin\/$/d' .dockerignore
docker build --platform linux/amd64 -t "${image}" -f Dockerfile.task-runner .
docker push "${image}"
docker inspect --format '{{index .RepoDigests 0}}' "${image}" > out/task-runner-image.txt
