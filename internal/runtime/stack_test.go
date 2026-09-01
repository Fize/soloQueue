package runtime

import (
	"sync"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
)

func TestSupervisorsSnapshotIsCopiedAndConcurrentSafe(t *testing.T) {
	s := &Stack{}
	leader := agent.NewAgent(agent.Definition{ID: "leader"}, &agenttest.FakeLLM{}, nil)
	sv := agent.NewSupervisor(leader, nil, nil)
	s.AddSupervisor(sv)
	snapshot := s.SupervisorsSnapshot()
	if len(snapshot) != 1 || snapshot[0] != sv {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	snapshot[0] = nil
	if got := s.SupervisorsSnapshot(); len(got) != 1 || got[0] != sv {
		t.Fatal("caller mutation changed runtime supervisor backing slice")
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				s.AddSupervisor(sv)
				_ = s.SupervisorsSnapshot()
				s.RemoveSupervisor(sv)
			}
		}()
	}
	wg.Wait()
}
