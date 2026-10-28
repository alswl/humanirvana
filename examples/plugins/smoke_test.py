"""Run from the repository root: python3 examples/plugins/smoke_test.py.

Requires Go, Python 3, POSIX signals, and an unused local TCP port 18080.
Builds the real executable so exit status is not obscured by `go run`.
"""

import signal
import socket
import subprocess
import tempfile
import time
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ADDRESS = ("127.0.0.1", 18080)
# Keep this local smoke test independent of the caller's HTTP proxy settings.
HTTP = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def check_signal(binary, termination_signal):
    process = subprocess.Popen([binary], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        deadline = time.monotonic() + 10
        while True:
            if process.poll() is not None:
                raise AssertionError((process.returncode, process.communicate()))
            try:
                with HTTP.open("http://127.0.0.1:18080/healthz", timeout=0.5) as response:
                    assert response.status == 200
                break
            except OSError:
                if time.monotonic() >= deadline:
                    raise AssertionError("HTTP startup timed out")
                time.sleep(0.05)
        with HTTP.open("http://127.0.0.1:18080/hello", timeout=2) as response:
            assert response.status == 200
            assert response.read() == b'"hello"\n'
        process.send_signal(termination_signal)
        stdout, stderr = process.communicate(timeout=10)
        assert process.returncode == 0, (process.returncode, stdout, stderr)
        assert not stderr, stderr
        print(f"{termination_signal.name}: HTTP served, graceful exit 0")
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()


with tempfile.TemporaryDirectory() as directory:
    binary = str(Path(directory) / "plugins")
    subprocess.run(["go", "build", "-o", binary, "./examples/plugins"], cwd=ROOT, check=True)
    with socket.socket() as occupied:
        occupied.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        occupied.bind(ADDRESS)
        occupied.listen()
        result = subprocess.run([binary], capture_output=True, timeout=10)
        assert result.returncode == 1, (result.returncode, result.stderr)
        assert b"bind" in result.stderr, result.stderr
        print("Occupied port: startup error logged, exit 1")
    for termination_signal in (signal.SIGTERM, signal.SIGINT):
        check_signal(binary, termination_signal)
