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
	"testing"
)

func TestInjectModelEnv(t *testing.T) {
	const url = "https://openrouter.ai/api/v1"
	secret := func(_ context.Context, ns, name, key string) (string, error) {
		if ns == "default" && name == modelSecretName && key == modelAPIKeyEnv {
			return "from-secret", nil
		}
		return "", nil
	}
	envOf := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	tests := []struct {
		name       string
		controller map[string]string
		task       map[string]string
		atespace   string
		want       map[string]string
	}{{
		name:     "no endpoint configured leaves env alone",
		atespace: "default",
		want:     map[string]string{},
	}, {
		name:       "controller endpoint plus key from atespace secret",
		controller: map[string]string{modelBaseURLEnv: url, modelNameEnv: "google/gemini-3.8-flash"},
		atespace:   "default",
		want:       map[string]string{modelBaseURLEnv: url, modelNameEnv: "google/gemini-3.8-flash", modelAPIKeyEnv: "from-secret"},
	}, {
		name:       "task values win",
		controller: map[string]string{modelBaseURLEnv: url, modelNameEnv: "a"},
		task:       map[string]string{modelNameEnv: "b", modelAPIKeyEnv: "task-key"},
		atespace:   "default",
		want:       map[string]string{modelBaseURLEnv: url, modelNameEnv: "b", modelAPIKeyEnv: "task-key"},
	}, {
		name:       "falls back to controller key when atespace has no secret",
		controller: map[string]string{modelBaseURLEnv: url, modelAPIKeyEnv: "controller-key"},
		atespace:   "other",
		want:       map[string]string{modelBaseURLEnv: url, modelAPIKeyEnv: "controller-key"},
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{}
			for k, v := range tc.task {
				env[k] = v
			}
			injectModelEnv(context.Background(), secret, envOf(tc.controller), tc.atespace, env)
			if len(env) != len(tc.want) {
				t.Fatalf("env = %v, want %v", env, tc.want)
			}
			for k, v := range tc.want {
				if env[k] != v {
					t.Errorf("env[%s] = %q, want %q", k, env[k], v)
				}
			}
		})
	}
}
