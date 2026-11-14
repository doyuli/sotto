import argparse
import os
from pathlib import Path
import sys
import tempfile
import time
import unittest

from performance import Sampler, native_reader, package_size, positive


class PerformanceTests(unittest.TestCase):
    def test_package_inventory_and_missing_build(self):
        # A missing/empty package must not be reported as a successful size cut.
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with self.assertRaises(ValueError):
                package_size(root / "missing")
            with self.assertRaises(ValueError):
                package_size(root)
            (root / "ocr").mkdir()
            (root / "Sotto.exe").write_bytes(b"app")
            (root / "ocr" / "model.onnx").write_bytes(b"model")
            report = package_size(root)
            self.assertEqual(report["total_bytes"], 8)
            self.assertCountEqual(report["files"], [
                {"path": "Sotto.exe", "bytes": 3}, {"path": "ocr/model.onnx", "bytes": 5}])

    def test_cpu_deltas_and_pid_reuse(self):
        # Never splice a replacement process into the original PID's timeline.
        readings = iter([{"start_id": 1, "cpu_ns": 10}, {"start_id": 1, "cpu_ns": 110},
                         {"start_id": 2, "cpu_ns": 5}])
        sampler = Sampler(lambda _: next(readings))
        self.assertIsNone(sampler.sample(7, 1000)["cpu_percent_one_core"])
        self.assertEqual(sampler.sample(7, 1200)["cpu_percent_one_core"], 50)
        with self.assertRaises(ProcessLookupError):
            sampler.sample(7, 1400)

    def test_reject_nonfinite_sampling_arguments(self):
        for value in ("nan", "inf", "-1", "0"):
            with self.assertRaises(argparse.ArgumentTypeError):
                positive(value)

    @unittest.skipUnless(sys.platform in ("darwin", "win32"), "native counters")
    def test_native_self_sample(self):
        # Read only this test process; no desktop, clipboard or app launch.
        read = native_reader().read
        before = time.process_time_ns()
        first = read(os.getpid())
        after = time.process_time_ns()
        # Apple Silicon's Mach ticks are not nanoseconds. Compare to Python's
        # independent process CPU clock so incorrect units cannot look valid.
        self.assertGreaterEqual(first["cpu_ns"], before - 2_000_000)
        self.assertLessEqual(first["cpu_ns"], after + 2_000_000)
        second = read(os.getpid())
        self.assertGreater(first["start_id"], 0)
        self.assertEqual(first["start_id"], second["start_id"])
        self.assertGreaterEqual(second["cpu_ns"], first["cpu_ns"])
        field = "physical_footprint_bytes" if sys.platform == "darwin" else "private_bytes"
        self.assertGreater(second[field], 0)


if __name__ == "__main__":
    unittest.main()
