#!/usr/bin/env python3
import re
import subprocess
import sys

from harness import fixture_path, run_tui, wait_for_server


def main():
    server = subprocess.Popen([sys.executable, str(fixture_path("repro_matrix_server.py"))])
    try:
        wait_for_server("http://127.0.0.1:18081/api/dashboards/uid/repro")

        matrix = run_tui(
            "http://127.0.0.1:18081/d/repro",
            "matrix-repro-release.txt",
        )
        for pattern in (
            r"cluster\s+NotReady\s+Cordoned, Ready\s+Not allocatable, Ready\s+Total",
            r"beta\s+5 GPU-h\s+6 GPU-h\s+7 GPU-h\s+8 GPU-h",
            r"alpha\s+1 GPU-h\s+2 GPU-h\s+3 GPU-h\s+4 GPU-h",
        ):
            assert re.search(pattern, matrix), pattern
        assert "Value #" not in matrix
        assert "TOTAL" in matrix
        assert "12 GPU-h" in matrix
        assert "invalid dashboard refresh" not in matrix

        budget = run_tui(
            "http://127.0.0.1:18081/d/repro?viewPanel=14",
            "calculate-field-repro.txt",
        )
        assert "Budget left" in budget
        assert "-0.50%" in budget
        assert "unsupported transformation" not in budget

        alerts = run_tui(
            "http://127.0.0.1:18081/d/repro?viewPanel=15",
            "alert-list-repro.txt",
        )
        assert "FIRING" in alerts
        assert "Disk full" in alerts
        assert "1 instance(s)" in alerts
        assert "Operations" in alerts
        assert 'severity="critical"' in alerts
        assert "Prometheus down" in alerts
        assert "not implemented" not in alerts

        groups = run_tui(
            "http://127.0.0.1:18081/d/repro?viewPanel=16",
            "alert-groups-repro.txt",
        )
        assert "severity=critical" in groups
        assert "severity=warning" in groups
        assert "custom grouping is not supported" not in groups
        assert "not implemented" not in groups

        series = run_tui(
            "http://127.0.0.1:18081/d/repro?viewPanel=17",
            "table-series-repro.txt",
        )
        assert "gpu-0" in series
        assert "gpu-1" in series
        assert "111" in series and "333" in series
        print("matrix headers and both rows: OK")
        print("refresh:false dashboard: OK")
        print("cross-query calculateField stat (-0.50%): OK")
        print("read-only alert-list API and instances: OK")
        print("multi-source alerts and custom grouping: OK")
        print("time-aligned multi-series table columns: OK")
    finally:
        server.terminate()
        server.wait(timeout=5)


if __name__ == "__main__":
    main()
