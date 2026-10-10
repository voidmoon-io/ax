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

package substrate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mockControlClient struct {
	ateapipb.ControlClient

	mu             sync.Mutex
	deleteCalls    []*ateapipb.DeleteActorRequest
	getCalls       []*ateapipb.GetActorRequest
	deleteErr      error
	actorRemaining int // Number of GetActor calls before returning NotFound
}

func (m *mockControlClient) DeleteActor(ctx context.Context, req *ateapipb.DeleteActorRequest, opts ...grpc.CallOption) (*ateapipb.Actor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteCalls = append(m.deleteCalls, req)
	if m.deleteErr != nil {
		return nil, m.deleteErr
	}
	return &ateapipb.Actor{}, nil
}

func (m *mockControlClient) GetActor(ctx context.Context, req *ateapipb.GetActorRequest, opts ...grpc.CallOption) (*ateapipb.Actor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getCalls = append(m.getCalls, req)
	if m.actorRemaining > 0 {
		m.actorRemaining--
		return &ateapipb.Actor{
			Metadata: &ateapipb.ResourceMetadata{
				Atespace: req.GetActor().GetAtespace(),
				Name:     req.GetActor().GetName(),
			},
			Status: &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_DELETING},
		}, nil
	}
	return nil, status.Error(codes.NotFound, "actor not found")
}

func TestClient_DeleteActor_AnyStateTrue(t *testing.T) {
	mock := &mockControlClient{
		actorRemaining: 0,
	}
	c := &Client{control: mock}
	ctx := context.Background()

	err := c.DeleteActor(ctx, "test-ns", "test-actor")
	if err != nil {
		t.Fatalf("DeleteActor failed: %v", err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.deleteCalls) != 1 {
		t.Fatalf("expected 1 DeleteActor call, got %d", len(mock.deleteCalls))
	}
	req := mock.deleteCalls[0]
	if !req.GetAnyState() {
		t.Errorf("expected AnyState to be true, got %v", req.GetAnyState())
	}
	if req.GetActor().GetAtespace() != "test-ns" || req.GetActor().GetName() != "test-actor" {
		t.Errorf("unexpected actor ref in request: %v", req.GetActor())
	}
}

func TestClient_DeleteActor_PollsUntilNotFound(t *testing.T) {
	oldInterval := actorDeletionPollInterval
	actorDeletionPollInterval = 10 * time.Millisecond
	defer func() { actorDeletionPollInterval = oldInterval }()

	mock := &mockControlClient{
		actorRemaining: 3,
	}
	c := &Client{control: mock}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := c.DeleteActor(ctx, "test-ns", "test-actor")
	if err != nil {
		t.Fatalf("DeleteActor failed: %v", err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	// 1 fast-path GetActor call + 3 polling calls (last one returns NotFound)
	if len(mock.getCalls) != 4 {
		t.Errorf("expected 4 GetActor calls before NotFound, got %d", len(mock.getCalls))
	}
}

func TestClient_DeleteActor_NotFoundIsIgnored(t *testing.T) {
	mock := &mockControlClient{
		deleteErr: status.Error(codes.NotFound, "actor already gone"),
	}
	c := &Client{control: mock}
	ctx := context.Background()

	err := c.DeleteActor(ctx, "test-ns", "test-actor")
	if err != nil {
		t.Fatalf("expected nil error when actor is NotFound, got: %v", err)
	}
}

func TestClient_DeleteActor_PropagatesRPCError(t *testing.T) {
	mock := &mockControlClient{
		deleteErr: errors.New("internal atelet failure"),
	}
	c := &Client{control: mock}
	ctx := context.Background()

	err := c.DeleteActor(ctx, "test-ns", "test-actor")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

type mockEgressControlClient struct {
	ateapipb.ControlClient

	existing *ateapipb.EgressPolicy
	creates  []*ateapipb.CreateActorEgressPolicyRequest
	updates  []*ateapipb.UpdateActorEgressPolicyRequest
}

func (m *mockEgressControlClient) GetActorEgressPolicy(ctx context.Context, req *ateapipb.GetActorEgressPolicyRequest, opts ...grpc.CallOption) (*ateapipb.EgressPolicy, error) {
	if m.existing == nil {
		return nil, status.Error(codes.NotFound, "egress policy not found")
	}
	return m.existing, nil
}

func (m *mockEgressControlClient) CreateActorEgressPolicy(ctx context.Context, req *ateapipb.CreateActorEgressPolicyRequest, opts ...grpc.CallOption) (*ateapipb.EgressPolicy, error) {
	m.creates = append(m.creates, req)
	return req.GetEgressPolicy(), nil
}

func (m *mockEgressControlClient) UpdateActorEgressPolicy(ctx context.Context, req *ateapipb.UpdateActorEgressPolicyRequest, opts ...grpc.CallOption) (*ateapipb.EgressPolicy, error) {
	m.updates = append(m.updates, req)
	return req.GetEgressPolicy(), nil
}

func hostnameRule(patterns ...string) *ateapipb.EgressRule {
	return &ateapipb.EgressRule{Hostnames: &ateapipb.HostnameRule{Patterns: patterns}}
}

// TestEnsureActorEgressPolicy covers the three cases a reconcile meets: an actor
// without a policy gets one, an unchanged policy is left alone, and a changed
// one is replaced carrying the server's metadata.
func TestEnsureActorEgressPolicy(t *testing.T) {
	rules := []*ateapipb.EgressRule{hostnameRule("model-proxy.ax-system.svc.cluster.local")}

	t.Run("creates", func(t *testing.T) {
		mock := &mockEgressControlClient{}
		c := &Client{control: mock}
		if err := c.EnsureActorEgressPolicy(context.Background(), "default", "task1", rules); err != nil {
			t.Fatal(err)
		}
		if len(mock.creates) != 1 || len(mock.updates) != 0 {
			t.Fatalf("expected 1 create and 0 updates, got %d and %d", len(mock.creates), len(mock.updates))
		}
		req := mock.creates[0]
		if req.GetActor().GetName() != "task1" || req.GetActor().GetAtespace() != "default" {
			t.Errorf("unexpected parent actor %v", req.GetActor())
		}
		md := req.GetEgressPolicy().GetMetadata()
		if md.GetName() != "default" || md.GetAtespace() != "default" {
			t.Errorf("expected policy default in the actor's atespace, got %v", md)
		}
	})

	t.Run("unchanged", func(t *testing.T) {
		mock := &mockEgressControlClient{existing: &ateapipb.EgressPolicy{Rules: rules}}
		c := &Client{control: mock}
		if err := c.EnsureActorEgressPolicy(context.Background(), "default", "task1", rules); err != nil {
			t.Fatal(err)
		}
		if len(mock.creates)+len(mock.updates) != 0 {
			t.Fatalf("expected no writes for an unchanged policy, got %d creates and %d updates", len(mock.creates), len(mock.updates))
		}
	})

	t.Run("updates", func(t *testing.T) {
		md := &ateapipb.ResourceMetadata{Atespace: "default", Name: "default", Version: 7}
		mock := &mockEgressControlClient{existing: &ateapipb.EgressPolicy{Metadata: md, Rules: []*ateapipb.EgressRule{hostnameRule("old.example.com")}}}
		c := &Client{control: mock}
		if err := c.EnsureActorEgressPolicy(context.Background(), "default", "task1", rules); err != nil {
			t.Fatal(err)
		}
		if len(mock.updates) != 1 {
			t.Fatalf("expected 1 update, got %d", len(mock.updates))
		}
		got := mock.updates[0].GetEgressPolicy()
		if got.GetMetadata().GetVersion() != 7 {
			t.Errorf("expected the server's metadata carried into the update, got %v", got.GetMetadata())
		}
		if !rulesEqual(got.GetRules(), rules) {
			t.Errorf("expected the new rules, got %v", got.GetRules())
		}
	})
}

// TestLoadEgressPolicy reads the protojson an operator mounts from a ConfigMap,
// and drops any metadata in it, since that is set per actor.
func TestLoadEgressPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	data := `{
  "metadata": {"name": "ignored"},
  "rules": [
    {"hostnames": {"patterns": ["model-proxy.ax-system.svc.cluster.local", "*.github.com"]}},
    {"cidrs": {"cidrs": ["0.0.0.0/5"]}}
  ]
}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadEgressPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if policy.GetMetadata() != nil {
		t.Errorf("expected metadata dropped, got %v", policy.GetMetadata())
	}
	if n := len(policy.GetRules()); n != 2 {
		t.Fatalf("expected 2 rules, got %d", n)
	}
	if got := policy.GetRules()[1].GetCidrs().GetCidrs(); len(got) != 1 || got[0] != "0.0.0.0/5" {
		t.Errorf("unexpected cidr rule %v", got)
	}

	if err := os.WriteFile(path, []byte(`{"rules": [{"nope": {}}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEgressPolicy(path); err == nil {
		t.Error("expected an error for an unknown field")
	}
}
