"""Runner process isolation checks; these do not load or build any SDK."""
import os
from pathlib import Path
import re
import signal
import sys
import tempfile
import time
import unittest

from run import Language, cell, run_process


@unittest.skipUnless(sys.platform.startswith("linux"), "Linux acceptance runner")
class ProcessTests(unittest.TestCase):
    def test_success_preserves_output(self):
        result = run_process(
            [sys.executable, "-c", "import sys; print('out'); print('err', file=sys.stderr)"],
            Path.cwd(), 5,
        )
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "out\n")
        self.assertEqual(result.stderr, "err\n")

    def test_timeout_kills_descendant_and_keeps_cell_output(self):
        # The wrapper exits but its child keeps the output pipes open. Killing
        # only the wrapper would leave a writer alive during the next cell.
        child = (
            "import os, sys, time; "
            "print('child-pid=' + str(os.getpid()), flush=True); "
            "print('partial-error', file=sys.stderr, flush=True); time.sleep(600)"
        )
        wrapper = f"import subprocess, sys; subprocess.Popen([sys.executable, '-c', {child!r}])"
        language = Language("test", [sys.executable, "-c", wrapper], True, "")
        with tempfile.TemporaryDirectory() as directory:
            result = cell("timeout", language, [], "", Path(directory), 1)
            log = result.log_path.read_text()
            self.assertEqual(result.status, "failed")
            self.assertIn("exit=124", log)
            self.assertIn("partial-error", log)
            self.assertIn("timed out after 1s", log)
            pid = int(re.search(r"^child-pid=(\d+)$", log, re.MULTILINE).group(1))
            try:
                # Reaping the orphan is host-owned; a zombie cannot write.
                deadline = time.monotonic() + 2
                while time.monotonic() < deadline:
                    try:
                        state = Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()[0]
                    except FileNotFoundError:
                        return
                    if state in ("Z", "X"):
                        return
                    time.sleep(0.01)
                self.fail("timed-out SDK descendant is still running")
            finally:
                try:
                    os.kill(pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass


if __name__ == "__main__":
    unittest.main()
