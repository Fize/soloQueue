import asyncio
import unittest
from unittest.mock import MagicMock, patch
from soloqueue.orchestration.orchestrator import Orchestrator
from soloqueue.orchestration.signals import ControlSignal, SignalType
from soloqueue.core.loaders.schema import AgentSchema
from soloqueue.orchestration.frame import TaskFrame
import os

class TestSkillFeature(unittest.TestCase):

    def setUp(self):
        self.registry = MagicMock()
        # Mock base agent
        agent = AgentSchema(
            name="base_agent", 
            description="test", 
            tools=["test-skill"]
        )
        self.registry.get_agent_by_node_id.return_value = agent
        
        # Create test skill
        self.skill_dir = "config/skills/test-skill"
        self.skill_file = f"{self.skill_dir}/SKILL.md"
        os.makedirs(self.skill_dir, exist_ok=True)
        with open(self.skill_file, "w") as f:
            f.write("---\n")
            f.write("name: test-skill\n")
            f.write("description: A test skill that echoes arguments\n")
            f.write("allowed_tools: []\n")
            f.write("---\n")
            f.write("You are a test skill.\n")
            f.write("User said: $ARGUMENTS\n")
            f.write('!echo "Skill Injection Working"\n')

    def tearDown(self):
        # Cleanup
        if os.path.exists(self.skill_file):
            os.remove(self.skill_file)
        if os.path.exists(self.skill_dir):
            os.rmdir(self.skill_dir)

    @patch("soloqueue.orchestration.orchestrator.AgentRunner")
    def test_skill_activation_flow(self, MockRunner):
        """
        Verifies that the Orchestrator correctly handles USE_SKILL signal:
        1. Loads the skill from disk
        2. Processes content (!cmd and $ARGUMENTS)
        3. Creates a dynamic TaskFrame
        4. Pushes it to stack
        """
        orchestrator = Orchestrator(self.registry)
        
        # Mock Runner behavior
        mock_instance = MockRunner.return_value
        
        # We expect 3 steps:
        mock_instance.step.side_effect = [
            ControlSignal(
                type=SignalType.USE_SKILL, 
                skill_name="test-skill", 
                skill_args="Authentication Token 123"
            ),
            ControlSignal(
                type=SignalType.RETURN,
                result="Skill Executed Successfully"
            ),
            ControlSignal(
                type=SignalType.RETURN,
                result="Final Task Complete"
            )
        ]
        
        # Run
        print("Starting Orchestrator Run...")
        result = asyncio.run(orchestrator.run("base_agent", "Please use the test skill"))
        
        self.assertEqual(result, "Final Task Complete")
        self.assertEqual(mock_instance.step.call_count, 3)
        
        # Verify the Skill Frame (2nd call)
        args, _ = mock_instance.step.call_args_list[1]
        skill_frame = args[0]
        
        print(f"Skill Frame Agent: {skill_frame.agent_name}")
        print(f"Skill System Prompt: {skill_frame.dynamic_config.system_prompt}")
        
        # Assertions
        self.assertEqual(skill_frame.agent_name, "skill__test-skill")
        self.assertIsNotNone(skill_frame.dynamic_config)
        
        # Check Content Processing
        prompt = skill_frame.dynamic_config.system_prompt
        self.assertIn("You are a test skill.", prompt)
        self.assertIn("Authentication Token 123", prompt) # $ARGUMENTS replacement
        self.assertIn("Skill Injection Working", prompt)  # !echo execution result
        
        # Check Tools
        self.assertEqual(skill_frame.dynamic_config.tools, [])

if __name__ == "__main__":
    unittest.main()
