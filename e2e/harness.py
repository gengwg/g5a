"""Linux PTY harness for synthetic, read-only terminal integration tests."""

import os
import re
import shlex
import subprocess
import tempfile
import time
import urllib.request
from pathlib import Path

E2E_DIR = Path(__file__).resolve().parent
_work_dir = os.environ.get("G5A_E2E_WORK_DIR")
if not _work_dir:
    _work_dir = tempfile.mkdtemp(prefix="g5a-e2e-")
WORK_DIR = Path(_work_dir)
os.environ["G5A_E2E_WORK_DIR"] = str(WORK_DIR)
WORK_DIR.mkdir(parents=True, exist_ok=True)
BINARY = Path(os.environ.get("G5A_BINARY", WORK_DIR / "g5a")).resolve()
ANSI = re.compile(r"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))")


def fixture_path(name):
    return E2E_DIR / name


def artifact(name):
    return WORK_DIR / name


def wait_for_server(url):
    for attempt in range(50):
        try:
            with urllib.request.urlopen(url, timeout=1):
                return
        except OSError:
            if attempt == 49:
                raise
            time.sleep(0.1)


def run_tui(url, output_path, wait_seconds=6, keys="", key_delay_seconds=0):
    if not BINARY.is_file():
        raise RuntimeError(f"terminal test binary not found: {BINARY}; set G5A_BINARY")
    output_path = Path(output_path)
    if not output_path.is_absolute():
        output_path = artifact(output_path)
    key_input = f"printf '%s' {shlex.quote(keys)}"
    if keys and key_delay_seconds:
        key_input = "; ".join(
            f"printf '%s' {shlex.quote(char)}; sleep {key_delay_seconds}"
            for char in keys
        )
    terminal_command = (
        f"stty cols 140 rows 35; {shlex.quote(str(BINARY))} --url {shlex.quote(url)}"
    )
    command = (
        f"(sleep {wait_seconds}; {key_input}; sleep 1; printf q) | "
        f"timeout -k 2 {wait_seconds + 6} "
        f"script -qec {shlex.quote(terminal_command)} {shlex.quote(str(output_path))} "
        ">/dev/null 2>&1"
    )
    result = subprocess.run(
        ["bash", "-c", command],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        timeout=wait_seconds + 14,
        check=False,
    )
    if result.returncode not in (0, 124):
        raise RuntimeError(
            f"terminal exited with status {result.returncode}: {result.stderr[-300:]!r}"
        )
    output = output_path.read_bytes()
    text = ANSI.sub("", output.decode("utf-8", errors="replace"))
    text = "\n".join(
        line
        for line in text.splitlines()
        if not line.startswith(("Script started on ", "Script done on "))
    )
    if not text.strip():
        raise RuntimeError(
            f"no terminal output; stdout={result.stdout[-300:]!r}; stderr={result.stderr[-300:]!r}"
        )
    output_path.write_text(text)
    return text
