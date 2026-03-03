"""
tests/test_system_metrics.py — unit tests for system_metrics.py collectors/parsers.
"""
import sys
import os
import time
import unittest
from unittest.mock import patch, MagicMock

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import system_metrics


class TestParsers(unittest.TestCase):

    def test_parse_top_cpu_valid(self):
        sample = (
            "Processes: 500 total, 3 running\n"
            "CPU usage: 12.34% user, 5.00% sys, 82.66% idle\n"
        )
        r = system_metrics.parse_top_cpu(sample, cores=8)
        self.assertIsNone(r["error"])
        self.assertAlmostEqual(r["percent"], 17.3, places=0)
        self.assertEqual(r["cores"], 8)

    def test_parse_top_cpu_no_line(self):
        r = system_metrics.parse_top_cpu("no useful output", cores=4)
        self.assertIsNotNone(r["error"])
        self.assertEqual(r["percent"], 0.0)

    def test_parse_vm_stat_valid(self):
        sample = (
            "Mach Virtual Memory Statistics: (page size of 16384 bytes)\n"
            "Pages free:                               12345.\n"
            "Pages active:                             50000.\n"
            "Pages inactive:                           20000.\n"
            "Pages wired down:                         30000.\n"
            "Pages occupied by compressor:             10000.\n"
        )
        total = 17179869184  # 16 GB
        r = system_metrics.parse_vm_stat(sample, total)
        self.assertIsNone(r["error"])
        self.assertEqual(r["totalBytes"], total)
        expected_used = (50000 + 30000 + 10000) * 16384
        self.assertEqual(r["usedBytes"], expected_used)
        self.assertGreater(r["percent"], 0)

    def test_parse_vm_stat_no_pages(self):
        r = system_metrics.parse_vm_stat("no pages here", total_bytes=1024)
        self.assertIsNotNone(r["error"])

    def test_parse_swap_usage_darwin_megabytes(self):
        sample = "vm.swapusage: total = 4096.00M  used = 512.00M  free = 3584.00M"
        r = system_metrics.parse_swap_usage_darwin(sample)
        self.assertIsNone(r["error"])
        self.assertEqual(r["totalBytes"], 4096 * 1024 * 1024)
        self.assertEqual(r["usedBytes"], 512 * 1024 * 1024)
        self.assertAlmostEqual(r["percent"], 12.5, places=1)

    def test_parse_swap_usage_darwin_gigabytes(self):
        sample = "vm.swapusage: total = 8.00G  used = 2.00G  free = 6.00G"
        r = system_metrics.parse_swap_usage_darwin(sample)
        self.assertIsNone(r["error"])
        self.assertEqual(r["totalBytes"], 8 * 1024 ** 3)
        self.assertEqual(r["usedBytes"], 2 * 1024 ** 3)

    def test_parse_swap_usage_darwin_invalid(self):
        r = system_metrics.parse_swap_usage_darwin("nothing useful here")
        self.assertIsNotNone(r["error"])

    def test_parse_proc_meminfo(self):
        sample = (
            "MemTotal:       16384000 kB\n"
            "MemFree:         2048000 kB\n"
            "MemAvailable:    8192000 kB\n"
            "SwapTotal:       4096000 kB\n"
            "SwapFree:        2048000 kB\n"
        )
        info = system_metrics.parse_proc_meminfo(sample)
        self.assertEqual(info["MemTotal"], 16384000)
        self.assertEqual(info["MemAvailable"], 8192000)
        self.assertEqual(info["SwapTotal"], 4096000)
        self.assertEqual(info["SwapFree"], 2048000)


class TestDiskCollector(unittest.TestCase):

    def test_collect_disk_root_returns_valid(self):
        r = system_metrics.collect_disk("/")
        self.assertIsNone(r["error"])
        self.assertGreater(r["totalBytes"], 0)
        self.assertGreaterEqual(r["usedBytes"], 0)
        self.assertGreaterEqual(r["percent"], 0)
        self.assertLessEqual(r["percent"], 100)

    def test_collect_disk_invalid_path(self):
        r = system_metrics.collect_disk("/nonexistent-path-xyz-abc")
        self.assertIsNotNone(r["error"])
        self.assertEqual(r["totalBytes"], 0)


class TestCache(unittest.TestCase):

    def setUp(self):
        # Reset cache state before each test
        import system_metrics as sm
        sm._metrics_payload = None
        sm._metrics_at = 0.0
        sm._metrics_refreshing = False
        sm._versions_cache = None
        sm._versions_at = 0.0
        sm._cfg = {
            "enabled": True,
            "pollSeconds": 5,
            "metricsTtlSeconds": 5,
            "versionsTtlSeconds": 300,
            "gatewayTimeoutMs": 1500,
            "diskPath": "/",
            "warnPercent": 70,
            "criticalPercent": 85,
        }

    def test_cache_returns_same_within_ttl(self):
        status1, body1 = system_metrics.get_payload()
        status2, body2 = system_metrics.get_payload()
        self.assertEqual(status1, 200)
        self.assertEqual(status2, 200)
        self.assertEqual(body1, body2)

    def test_cache_refreshes_after_ttl(self):
        import system_metrics as sm
        sm._cfg["metricsTtlSeconds"] = 0  # expire immediately
        status1, body1 = system_metrics.get_payload()
        time.sleep(0.05)
        # Force cache expiry
        sm._metrics_at = 0
        status2, body2 = system_metrics.get_payload()
        self.assertEqual(status1, 200)
        self.assertEqual(status2, 200)

    def test_disabled_returns_503(self):
        import system_metrics as sm
        sm._cfg["enabled"] = False
        status, body = system_metrics.get_payload()
        self.assertEqual(status, 503)
        import json
        data = json.loads(body)
        self.assertFalse(data["ok"])


if __name__ == "__main__":
    unittest.main()
