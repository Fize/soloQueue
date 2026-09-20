package agent

import "testing"

func TestResolveTargetPrefersCanonicalIDAndAcceptsDisplayName(t *testing.T) {
	refs := []TargetRef{{ID: "andrej_karpathy", DisplayName: "Andrej Karpathy", Kind: TargetLeader}}
	if got, ok := ResolveTarget("andrej_karpathy", refs); !ok || got.ID != "andrej_karpathy" {
		t.Fatalf("canonical target did not resolve: %#v %v", got, ok)
	}
	if got, ok := ResolveTarget("Andrej Karpathy", refs); !ok || got.ID != "andrej_karpathy" {
		t.Fatalf("display name did not resolve to canonical ID: %#v %v", got, ok)
	}
	if _, ok := ResolveTarget("unknown", refs); ok {
		t.Fatal("unknown target resolved unexpectedly")
	}
}

func TestVisibleTargetsSeparateWorkersAndPeerLeaders(t *testing.T) {
	self := AgentTemplate{ID: "engineering", Name: "Engineering", Group: "engineering", IsLeader: true}
	templates := map[string]AgentTemplate{
		"engineering":   self,
		"editor":        {ID: "editor", Name: "Editor", Group: "engineering"},
		"engineering-2": {ID: "engineering-2", Name: "Engineering 2", Group: "engineering", IsLeader: true},
		"design":        {ID: "design", Name: "Design", Group: "design", IsLeader: true},
	}
	workers := VisibleWorkerTargets(templates, self, nil)
	peers := VisibleLeaderTargets(templates, self)
	if len(workers) != 1 || workers[0].ID != "editor" || workers[0].Kind != TargetWorker {
		t.Fatalf("workers = %#v", workers)
	}
	if len(peers) != 1 || peers[0].ID != "design" || peers[0].Kind != TargetLeader {
		t.Fatalf("peers = %#v", peers)
	}
}
