"""Process and temporary-site helpers shared by the real-browser tests."""

from contextlib import contextmanager
from pathlib import Path
import selectors
import shutil
import subprocess
import tempfile
import time
from types import SimpleNamespace
from urllib.error import URLError
from urllib.request import urlopen


@contextmanager
def managed_site(binary, startup_timeout=10):
    """Start an empty SQLite site on an ephemeral loopback port and reap it."""
    root = Path(tempfile.mkdtemp(prefix="composure-browser-"))
    process = None
    try:
        subprocess.run(
            [str(binary), "init", "--site", str(root), "--apply"],
            check=True,
            capture_output=True,
            text=True,
        )
        process = subprocess.Popen(
            [str(binary), "serve", "--site", str(root), "--addr", "127.0.0.1:0"],
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
        )
        selector = selectors.DefaultSelector()
        try:
            selector.register(process.stdout, selectors.EVENT_READ)
            if not selector.select(startup_timeout):
                raise RuntimeError(
                    f"server did not print its ephemeral address within {startup_timeout}s"
                )
            line = process.stdout.readline().strip()
            if not line.startswith("listening on "):
                raise RuntimeError(f"server printed no usable address: {line!r}")
            address = line.removeprefix("listening on ")
        finally:
            selector.close()

        deadline = time.monotonic() + startup_timeout
        while True:
            if process.poll() is not None:
                raise RuntimeError(f"server exited before readiness with status {process.returncode}")
            try:
                with urlopen(address + "/healthz", timeout=0.25) as response:
                    if response.status == 200 and response.read().strip() == b"ok":
                        break
            except (OSError, URLError):
                pass
            if time.monotonic() >= deadline:
                raise RuntimeError(f"server health check did not pass within {startup_timeout}s")
            time.sleep(0.05)

        yield SimpleNamespace(root=root, url=address, process=process)
    finally:
        if process is not None:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
            if process.stdout is not None:
                process.stdout.close()
        shutil.rmtree(root, ignore_errors=True)
