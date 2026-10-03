"""Process and temporary-site helpers shared by the real-browser tests."""

from contextlib import contextmanager
from pathlib import Path
import os
import selectors
import shutil
import socket
import ssl
import subprocess
import tempfile
import time
from types import SimpleNamespace
from urllib.error import URLError
from urllib.request import urlopen


ADMIN_EMAIL = "browser@example.test"
ADMIN_PASSWORD = "Browser-fixture-password-2026!"


def _announcement(process, startup_timeout):
    deadline = time.monotonic() + startup_timeout
    selector = selectors.DefaultSelector()
    try:
        output_fd = process.stdout.fileno()
        os.set_blocking(output_fd, False)
        selector.register(output_fd, selectors.EVENT_READ)
        line = bytearray()
        while b"\n" not in line:
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not selector.select(remaining):
                raise RuntimeError(f"process did not print a complete address within {startup_timeout}s")
            try:
                chunk = os.read(output_fd, 4096)
            except BlockingIOError:
                continue
            if not chunk:
                raise RuntimeError("process exited before printing its address")
            line.extend(chunk)
    finally:
        selector.close()
    announced = bytes(line).split(b"\n", 1)[0].decode("utf-8", errors="replace").strip()
    if not announced.startswith("listening on "):
        raise RuntimeError(f"process printed no usable address: {announced!r}")
    return announced.removeprefix("listening on ")


def _wait_ready(address, process, startup_timeout):
    deadline = time.monotonic() + startup_timeout
    while True:
        if process.poll() is not None:
            raise RuntimeError(f"server exited before readiness with status {process.returncode}")
        try:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise RuntimeError(f"server health check did not pass within {startup_timeout}s")
            context = ssl._create_unverified_context() if address.startswith("https://") else None
            with urlopen(address + "/healthz", timeout=min(0.25, remaining), context=context) as response:
                if response.status == 200 and response.read().strip() == b"ok":
                    return
        except (OSError, URLError):
            pass
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise RuntimeError(f"server health check did not pass within {startup_timeout}s")
        time.sleep(min(0.05, remaining))


def _stop(process):
    if process is None:
        return
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)
    if process.stdout is not None:
        process.stdout.close()


@contextmanager
def managed_site(binary, startup_timeout=10, tls_proxy_binary=None):
    """Start a CLI site, optionally behind a same-port IPv6 TLS proxy."""
    root = Path(tempfile.mkdtemp(prefix="composure-browser-"))
    process = None
    proxy_process = None
    try:
        init = [str(binary), "init", "--site", str(root), "--apply"]
        environment = os.environ.copy()
        if tls_proxy_binary is not None:
            init.extend(["--admin-email", ADMIN_EMAIL])
            environment["COMPOSURE_ADMIN_PASSWORD"] = ADMIN_PASSWORD
        subprocess.run(init, check=True, capture_output=True, text=True, env=environment)

        if tls_proxy_binary is None:
            cli_address = "127.0.0.1:0"
        else:
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as reserved:
                reserved.bind(("127.0.0.1", 0))
                port = reserved.getsockname()[1]
            cli_address = f"127.0.0.1:{port}"
        process = subprocess.Popen(
            [str(binary), "serve", "--site", str(root), "--addr", cli_address],
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
        )
        cli_url = _announcement(process, startup_timeout)
        _wait_ready(cli_url, process, startup_timeout)
        address = cli_url

        if tls_proxy_binary is not None:
            proxy_process = subprocess.Popen(
                [
                    str(tls_proxy_binary), "--addr", f"[::1]:{port}",
                    "--upstream", cli_url,
                ],
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
            )
            address = _announcement(proxy_process, startup_timeout)
            _wait_ready(address, proxy_process, startup_timeout)

        yield SimpleNamespace(root=root, url=address, process=process, proxy_process=proxy_process)
    finally:
        _stop(proxy_process)
        _stop(process)
        shutil.rmtree(root, ignore_errors=True)
