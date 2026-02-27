"""AC28: flake.nix structure validation."""
import os
import unittest

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FLAKE = os.path.join(REPO, "flake.nix")


class TestNixFlake(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        with open(FLAKE) as f:
            cls.nix = f.read()

    def test_ac28a_has_description(self):
        self.assertIn("description", self.nix)
        self.assertIn("openclaw", self.nix.lower())

    def test_ac28b_nixpkgs_input(self):
        self.assertIn("github:NixOS/nixpkgs", self.nix)

    def test_ac28c_dev_shell_defined(self):
        self.assertIn("devShells", self.nix)
        self.assertIn("python3", self.nix)

    def test_ac28d_packages_default(self):
        self.assertIn("packages", self.nix)
        self.assertIn("default", self.nix)

    def test_ac28e_server_py_referenced(self):
        self.assertIn("server.py", self.nix)


if __name__ == "__main__":
    unittest.main()
