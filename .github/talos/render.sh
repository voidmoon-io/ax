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
mkdir -p out
cp deploy/redis.yaml out/
# Upstream folded ax-controller into ax-server after v0.3.1
# (google/ax@ac23328); older refs still ship it.
if [[ -f deploy/ax-controller.yaml ]]; then
  ko resolve -B -t "${TAG}" -f deploy/ax-controller.yaml > out/ax-controller.yaml
fi
ko resolve -B -t "${TAG}" -f deploy/ax-server.yaml > out/ax-server.yaml
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o out/ax ./cmd/ax
git rev-parse HEAD > out/SOURCE_COMMIT
