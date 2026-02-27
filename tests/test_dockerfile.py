"""AC27: Dockerfile structure validation."""
import os
import re
import unittest

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DOCKERFILE = os.path.join(REPO, "Dockerfile")


class TestDockerfile(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        with open(DOCKERFILE) as f:
            cls.df = f.read()

    def test_ac27a_base_image_python_slim(self):
        self.assertRegex(self.df, r'FROM python:\d+\.\d+-slim')

    def test_ac27b_workdir_app(self):
        self.assertIn("WORKDIR /app", self.df)

    def test_ac27c_copies_required_files(self):
        for f in ("index.html", "server.py", "refresh.sh", "themes.json"):
            self.assertIn(f, self.df)

    def test_ac27d_exposes_8080(self):
        self.assertIn("EXPOSE 8080", self.df)

    def test_ac27e_nonroot_user(self):
        self.assertRegex(self.df, r'USER\s+\w+')

    def test_ac27f_volume_declared(self):
        self.assertIn("VOLUME", self.df)


if __name__ == "__main__":
    unittest.main()
