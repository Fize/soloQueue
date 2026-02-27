
import asyncio
import sys
from unittest.mock import MagicMock
from langchain_core.messages import HumanMessage, ToolMessage
from soloqueue.orchestration.orchestrator import Orchestrator
from soloqueue.orchestration.signals import ControlSignal, SignalType
from soloqueue.core.loaders.schema import AgentSchema
from soloqueue.orchestration.frame import TaskFrame

# Mock Registry
class MockRegistry:
    def __init__(self):
        self.agents = {}
    
    def register(self, config):
        self.agents[config.node_id] = config
        
    def get_agent_by_node_id(self, node_id):
        return self.agents.get(node_id)
        
    def get_agent_by_name(self, name, current_group=None):
        # Simple mock resolution
        return self.get_agent_by_node_id(name)

def test_permissions():
    registry = MockRegistry()
    
    # Define Agents
    # Group A
    agent_a_leader = AgentSchema(name="leader", group="inv", is_leader=True, description="")
    agent_a_member = AgentSchema(name="analyst", group="inv", is_leader=False, description="")
    
    # Group B
    agent_b_leader = AgentSchema(name="leader", group="code", is_leader=True, description="")
    agent_b_member = AgentSchema(name="dev", group="code", is_leader=False, description="")
    
    registry.register(agent_a_leader)
    registry.register(agent_a_member)
    registry.register(agent_b_leader)
    registry.register(agent_b_member)
    
    orchestrator = Orchestrator(registry)
    
    # Test Cases
    scenarios = [
        ("inv__leader", "inv__analyst", True, "Intra-group (Leader -> Member)"),
        ("inv__analyst", "inv__leader", True, "Intra-group (Member -> Leader)"),
        ("inv__leader", "code__leader", True, "Inter-group (Leader -> Leader)"),
        ("inv__leader", "code__dev", False, "Inter-group (Leader -> Member) - SHOULD FAIL"),
        ("inv__analyst", "code__leader", False, "Inter-group (Member -> Leader) - SHOULD FAIL"),
    ]
    
    print("Running Permission Tests...\n")
    
    for source, target, expected_allow, desc in scenarios:
        print(f"Testing: {desc}")
        
        # Setup Frame
        orchestrator.stack = [] # Reset stack
        # Do NOT manually append frame here. run() does it.
        
        # Simulate DELEGATE signal
        signal = ControlSignal(
            type=SignalType.DELEGATE,
            target_agent=target,
            instruction="do something",
            tool_call_id="call_123"
        )
        
        # Mock _execute_frame to return our signal, then stop iteration
        orchestrator._execute_frame = MagicMock(side_effect=[signal, StopIteration])
        
        try:
            asyncio.run(orchestrator.run(source, "initial input"))
        except StopIteration:
            pass # Loop broken as expected
        except Exception as e:
            print(f"  ERROR: {e}")
            
        # Check Result
        # Run() initializes root + (if allowed) Child
        # Allowed: Stack len 2
        # Denied: Stack len 1
        
        stack_len = len(orchestrator.stack)
        
        if expected_allow:
            if stack_len == 2:
                top_agent = orchestrator.stack[-1].agent_name
                if top_agent == target:
                    print("  ✅ PASSED (Delegation succeeded)")
                else:
                    print(f"  ❌ FAILED (Wrong agent on top: {top_agent})")
            else:
                 print(f"  ❌ FAILED (Stack length is {stack_len}, expected 2)")
                 if stack_len == 1:
                     print(f"     Memory[-1]: {orchestrator.stack[-1].memory[-1]}")
        else:
            if stack_len == 1:
                last_msg = orchestrator.stack[-1].memory[-1]
                content = ""
                if hasattr(last_msg, "content"):
                    content = str(last_msg.content)
                
                if "Permission Denied" in content:
                    print("  ✅ PASSED (Correctly denied with message)")
                else:
                    print(f"  ❌ FAILED (Denied but no error message found: {content})")
            else:
                print(f"  ❌ FAILED (Delegation allowed unexpectedly. Stack len: {stack_len})")
                
        print("-" * 30)

if __name__ == "__main__":
    test_permissions()
