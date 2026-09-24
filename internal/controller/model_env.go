// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"log/slog"
	"os"
)

const (
	// modelSecretName holds the API key for an OpenAI-compatible model
	// endpoint, looked up in the namespace named after the task's atespace,
	// the same way gemini-api-secret is.
	modelSecretName = "ax-model-secret"
	modelAPIKeyEnv  = "AX_MODEL_API_KEY"
	modelBaseURLEnv = "AX_MODEL_BASE_URL"
	modelNameEnv    = "AX_MODEL_NAME"
)

// injectModelEnv points the sandbox agent at the controller's configured
// OpenAI-compatible endpoint, so tasks need not carry the endpoint -- or its key,
// which would otherwise sit in the task spec -- in spec.env. Values a task sets
// itself win. The key is resolved only when an endpoint is configured.
func injectModelEnv(ctx context.Context, resolve SecretResolver, getenv func(string) string, atespace string, env map[string]string) {
	for _, name := range []string{modelBaseURLEnv, modelNameEnv} {
		if env[name] == "" {
			if v := getenv(name); v != "" {
				env[name] = v
			}
		}
	}
	if env[modelBaseURLEnv] == "" || env[modelAPIKeyEnv] != "" {
		return
	}
	if resolve != nil {
		lookupCtx, cancel := context.WithTimeout(ctx, secretLookupTimeout)
		defer cancel()
		if key, err := resolve(lookupCtx, atespace, modelSecretName, modelAPIKeyEnv); err == nil && key != "" {
			slog.Info("resolved AX_MODEL_API_KEY from kubernetes secret for actor template", "atespace", atespace)
			env[modelAPIKeyEnv] = key
			return
		}
	}
	if key := getenv(modelAPIKeyEnv); key != "" {
		env[modelAPIKeyEnv] = key
	}
}

// osGetenv is swapped out in tests.
var osGetenv = os.Getenv
