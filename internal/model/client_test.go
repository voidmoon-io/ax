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

package model_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/ax/internal/model"
	"github.com/google/ax/internal/store/memory"
	"github.com/google/ax/pkg/apis/v1alpha1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestClient_Default(t *testing.T) {
	client := model.NewDefaultClient(model.WithDisableRemote(true))
	if client.Config().Model != model.DefaultModel {
		t.Fatalf("expected default model %q, got %q", model.DefaultModel, client.Config().Model)
	}
	if client.Config().Provider != model.ProviderGoogle {
		t.Fatalf("expected provider %q, got %q", model.ProviderGoogle, client.Config().Provider)
	}
	if client.Config().Name != model.DefaultModelResourceName {
		t.Fatalf("expected default name %q, got %q", model.DefaultModelResourceName, client.Config().Name)
	}
	if client.Config().SecretKey == nil {
		t.Fatalf("expected default secretKey not nil")
	}
	if client.Config().SecretKey.Name != model.DefaultSecretName || client.Config().SecretKey.Key != model.DefaultSecretKey {
		t.Errorf("expected secretKey %s/%s, got %s/%s", model.DefaultSecretName, model.DefaultSecretKey, client.Config().SecretKey.Name, client.Config().SecretKey.Key)
	}
	if len(client.Config().Parameters) != 0 {
		t.Errorf("expected default parameters to be empty, got %v", client.Config().Parameters)
	}

	resp, err := client.Generate(context.Background(), &model.GenerateRequest{
		Prompt: "Configure Go workspace",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Model != model.DefaultModel {
		t.Errorf("expected model %q, got %q", model.DefaultModel, resp.Model)
	}
	if resp.Content == "" {
		t.Errorf("expected non-empty content")
	}
}

func TestClient_FromConfig(t *testing.T) {
	cfg := model.Config{
		Provider: "google",
		Model:    "gemini-3.8-flash",
		Parameters: map[string]any{
			"temperature":       0.3,
			"maxOutputTokens":   2048,
			"systemInstruction": "System workspace planner",
		},
		DisableRemote: true,
	}

	client := model.NewClient(cfg)
	if client.Config().Model != "gemini-3.8-flash" {
		t.Errorf("expected model gemini-3.8-flash, got %s", client.Config().Model)
	}
	if client.Config().Parameters["temperature"] != 0.3 {
		t.Errorf("expected temperature 0.3, got %v", client.Config().Parameters["temperature"])
	}

	resp, err := client.Generate(context.Background(), &model.GenerateRequest{
		Prompt: "Bootstrap workspace",
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if resp.Model != "gemini-3.8-flash" {
		t.Errorf("expected gemini-3.8-flash, got %s", resp.Model)
	}
}

func TestClient_FromCRD(t *testing.T) {
	crd := &v1alpha1.Model{
		Metadata: &v1alpha1.ObjectMeta{
			Name:     "gemini-flash",
			Atespace: "default",
		},
		Spec: &v1alpha1.ModelSpec{
			Provider:   "google",
			Model:      "gemini-3.8-flash",
			Parameters: params(t, map[string]any{"temperature": 0.4, "maxOutputTokens": 1024}),
		},
	}

	client := model.NewClientFromCRD(crd, model.WithDisableRemote(true))
	if client.Config().Model != "gemini-3.8-flash" {
		t.Errorf("expected model gemini-3.8-flash, got %s", client.Config().Model)
	}

	resp, err := client.Generate(context.Background(), &model.GenerateRequest{
		Prompt: "Bootstrap workspace",
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if resp.Model != "gemini-3.8-flash" {
		t.Errorf("expected gemini-3.8-flash, got %s", resp.Model)
	}
}

func TestClient_GeminiHTTP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !r.URL.Query().Has("key") {
			t.Errorf("expected key in query params")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"candidates": []map[string]interface{}{
				{
					"content": map[string]interface{}{
						"parts": []map[string]string{
							{"text": "Setup Go environment with Go 1.27"},
						},
					},
				},
			},
			"usageMetadata": map[string]int{
				"promptTokenCount":     10,
				"candidatesTokenCount": 20,
				"totalTokenCount":      30,
			},
		})
	}))
	defer ts.Close()

	client := model.NewClient(
		model.Config{
			Provider: "google",
			Model:    "gemini-3.8-flash",
			BaseURL:  ts.URL,
			APIKey:   "test-api-key",
		},
		model.WithHTTPClient(ts.Client()),
	)

	resp, err := client.Generate(context.Background(), &model.GenerateRequest{
		Prompt: "Plan Go setup",
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if resp.Content != "Setup Go environment with Go 1.27" {
		t.Errorf("unexpected content: %q", resp.Content)
	}
	if resp.Usage.TotalTokens != 30 {
		t.Errorf("expected 30 total tokens, got %d", resp.Usage.TotalTokens)
	}
}

func TestClient_DefaultModelSecretResolution(t *testing.T) {
	// 1. Resolve via in-cluster Kubernetes API simulation
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/namespaces/default/secrets/gemini-api-secret" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]string{
					"GEMINI_API_KEY": "a3ViZXJuZXRlcy1zZWNyZXQtdmFsdWUtNTU1", // base64 for "kubernetes-secret-value-555"
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	// Parse host and port from test server URL
	hostPort := strings.TrimPrefix(ts.URL, "http://")
	parts := strings.Split(hostPort, ":")
	t.Setenv("KUBERNETES_SERVICE_HOST", parts[0])
	t.Setenv("KUBERNETES_SERVICE_PORT", parts[1])

	// Create temp token file
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	_ = os.WriteFile(tokenFile, []byte("fake-token"), 0644)

	// 2. Resolve via WithSecretResolver (standard in-memory / test pattern)
	clientResolver := model.NewDefaultClient(
		model.WithSecretResolver(func(secretName, key string) (string, error) {
			if secretName == "gemini-api-secret" && key == "GEMINI_API_KEY" {
				return "resolved-via-custom-func-333", nil
			}
			return "", nil
		}),
		model.WithDisableRemote(true),
	)
	if clientResolver.Config().APIKey != "resolved-via-custom-func-333" {
		t.Errorf("expected APIKey from custom resolver, got %q", clientResolver.Config().APIKey)
	}
}

func TestClient_DefaultModelStore(t *testing.T) {
	ctx := context.Background()
	s := memory.NewStore()

	// Store a customized default-model CRD
	customModel := &v1alpha1.Model{
		Metadata: &v1alpha1.ObjectMeta{
			Name:     "default-model",
			Atespace: "default",
		},
		Spec: &v1alpha1.ModelSpec{
			Provider:   "google",
			Model:      "gemini-3.8-flash",
			Parameters: params(t, map[string]any{"temperature": 0.7, "maxOutputTokens": 4096}),
			SecretKey: &v1alpha1.SecretKeyRef{
				Name: "custom-gemini-secret",
				Key:  "CUSTOM_KEY",
			},
		},
	}
	if err := s.SaveModel(ctx, customModel); err != nil {
		t.Fatalf("failed to save model: %v", err)
	}

	secretResolverOpt := model.WithSecretResolver(func(secretName, key string) (string, error) {
		if secretName == "custom-gemini-secret" && key == "CUSTOM_KEY" {
			return "store-secret-resolved-999", nil
		}
		return "", nil
	})

	// Read using NewDefaultClientFromStore
	client, err := model.NewDefaultClientFromStore(ctx, s, "default", secretResolverOpt, model.WithDisableRemote(true))
	if err != nil {
		t.Fatalf("NewDefaultClientFromStore failed: %v", err)
	}

	if client.Config().Name != "default-model" {
		t.Errorf("expected name default-model, got %s", client.Config().Name)
	}
	if client.Config().Parameters["temperature"] != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", client.Config().Parameters["temperature"])
	}
	if client.Config().Parameters["maxOutputTokens"] != float64(4096) {
		t.Errorf("expected maxOutputTokens 4096, got %v", client.Config().Parameters["maxOutputTokens"])
	}
	if client.Config().APIKey != "store-secret-resolved-999" {
		t.Errorf("expected APIKey 'store-secret-resolved-999', got %q", client.Config().APIKey)
	}

	// Read using WithStore option
	clientWithStore := model.NewDefaultClient(
		model.WithStore(ctx, s, "default", "default-model"),
		secretResolverOpt,
		model.WithDisableRemote(true),
	)
	if clientWithStore.Config().Parameters["temperature"] != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", clientWithStore.Config().Parameters["temperature"])
	}
	if clientWithStore.Config().APIKey != "store-secret-resolved-999" {
		t.Errorf("expected APIKey 'store-secret-resolved-999', got %q", clientWithStore.Config().APIKey)
	}
}

// TestClient_OpenAIProvider exercises the OpenAI-compatible path a self-hosted
// vLLM server serves: the base URL arrives through spec.parameters, the request
// is a chat completion, and the key is optional.
func TestClient_OpenAIProvider(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"plan ready"}}],`+
			`"usage":{"prompt_tokens":11,"completion_tokens":3,"total_tokens":14}}`)
	}))
	defer ts.Close()

	crd := &v1alpha1.Model{
		Metadata: &v1alpha1.ObjectMeta{Name: "local-vllm", Atespace: "default"},
		Spec: &v1alpha1.ModelSpec{
			Provider: "openai",
			Model:    "Qwen/Qwen3-4B-Instruct-2507",
			Parameters: params(t, map[string]any{
				"baseURL":     ts.URL + "/v1",
				"temperature": 0.2,
			}),
		},
	}

	client := model.NewClientFromCRD(crd)
	if got := client.Config().BaseURL; got != ts.URL+"/v1" {
		t.Errorf("expected base URL %q lifted out of parameters, got %q", ts.URL+"/v1", got)
	}

	resp, err := client.Generate(context.Background(), &model.GenerateRequest{Prompt: "Bootstrap workspace"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("expected /v1/chat/completions, got %s", gotPath)
	}
	if gotAuth != "" {
		t.Errorf("expected no Authorization header without a key, got %q", gotAuth)
	}
	if gotBody["model"] != "Qwen/Qwen3-4B-Instruct-2507" {
		t.Errorf("expected the served model name in the request, got %v", gotBody["model"])
	}
	if gotBody["temperature"] != 0.2 {
		t.Errorf("expected temperature 0.2 passed through, got %v", gotBody["temperature"])
	}
	if _, ok := gotBody["baseURL"]; ok {
		t.Error("baseURL should be lifted out of the parameters, not sent to the provider")
	}
	if resp.Content != "plan ready" {
		t.Errorf("expected content 'plan ready', got %q", resp.Content)
	}
	if resp.Usage.TotalTokens != 14 {
		t.Errorf("expected 14 total tokens, got %d", resp.Usage.TotalTokens)
	}
}

// TestClient_OpenAIProviderRequiresBaseURL checks the error a Model without a
// base URL produces, since there is no sensible default endpoint.
func TestClient_OpenAIProviderRequiresBaseURL(t *testing.T) {
	client := model.NewClient(model.Config{Provider: "openai", Model: "local"})
	if _, err := client.Generate(context.Background(), &model.GenerateRequest{Prompt: "hi"}); err == nil {
		t.Fatal("expected an error when no base URL is configured")
	}
}

// params builds a Struct for a ModelSpec's parameters, failing the test on
// unsupported value types.
func params(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	s, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
