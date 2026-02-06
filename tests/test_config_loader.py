import unittest
import tempfile
import shutil
import yaml
import os
from pathlib import Path
from soloqueue.core.config_loader import ConfigLoader

class TestConfigLoader(unittest.TestCase):
    def setUp(self):
        self.test_dir = tempfile.mkdtemp()
        self.config_root = Path(self.test_dir)
        (self.config_root / "groups").mkdir()
        (self.config_root / "agents").mkdir()

    def tearDown(self):
        shutil.rmtree(self.test_dir)

    def create_group(self, name):
        with open(self.config_root / "groups" / f"{name}.yaml", "w") as f:
            yaml.dump({"name": name, "description": "desc"}, f)

    def create_agent(self, name, group, is_leader=False):
        with open(self.config_root / "agents" / f"{name}_{group}.yaml", "w") as f:
            yaml.dump({
                "name": name, 
                "group": group, 
                "is_leader": is_leader,
                "system_prompt": "prompt"
            }, f)

    def test_basic_load(self):
        self.create_group("test_group")
        self.create_agent("a1", "test_group", is_leader=True)
        self.create_agent("a2", "test_group", is_leader=False)

        loader = ConfigLoader(self.test_dir)
        loader.load_all()

        self.assertIn("test_group", loader.groups)
        self.assertEqual(len(loader.agents), 2)
        
        agents = loader.get_agents_by_group("test_group")
        leader = next(a for a in agents if a.name == "a1")
        self.assertTrue(leader.is_leader)

    def test_orphan_agent(self):
        # Agent with non-existent group
        self.create_agent("orphan", "missing_group")
        
        loader = ConfigLoader(self.test_dir)
        loader.load_all()
        
        self.assertEqual(len(loader.agents), 0)

    def test_multiple_leaders(self):
        self.create_group("g1")
        # Creating two leaders. Assuming file loading order (alphabetic usually, but glob order varies)
        # We named files such that we can try to predict, but loader order depends on OS.
        # The logic is "First Loaded Wins".
        self.create_agent("leader1", "g1", is_leader=True)
        self.create_agent("leader2", "g1", is_leader=True)

        loader = ConfigLoader(self.test_dir)
        loader.load_all()

        agents = loader.get_agents_by_group("g1")
        leaders = [a for a in agents if a.is_leader]
        
        # Should strictly be 1 leader
        self.assertEqual(len(leaders), 1)
        # Total agents should be 2 (one downgraded)
        self.assertEqual(len(agents), 2)

if __name__ == "__main__":
    unittest.main()
