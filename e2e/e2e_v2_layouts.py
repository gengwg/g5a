#!/usr/bin/env python3
import re
import subprocess
import sys

from harness import fixture_path, run_tui, wait_for_server

server = subprocess.Popen([sys.executable, str(fixture_path("repro_matrix_server.py"))])
try:
    wait_for_server("http://127.0.0.1:18081/apis/dashboard.grafana.app/v2/namespaces/default/dashboards/v2demo")

    auto = run_tui("http://127.0.0.1:18081/d/v2auto", "v2-auto-repro.txt")
    assert "V2 Matrix Reproduction" in auto
    assert "NotReady" in auto

    tabs = run_tui("http://127.0.0.1:18081/d/v2tabs?viewPanel=24", "v2-tabs-repro.txt")
    assert "Details" in tabs

    library = run_tui("http://127.0.0.1:18081/d/v2library", "v2-library-repro.txt")
    assert "NotReady" in library and "alpha" in library and "beta" in library

    geo = run_tui("http://127.0.0.1:18081/d/v2demo?viewPanel=18", "geo-repro.txt")
    assert "San Francisco" in geo and "Berlin" in geo
    assert "37.7749" in geo and "52.5200" in geo

    vendor = run_tui("http://127.0.0.1:18081/d/v2demo?viewPanel=19", "vendor-repro.txt")
    assert "Supermicro" in vendor and "Dell Inc." in vendor and "Lenovo" in vendor
    assert "Value{" not in vendor

    gauge = run_tui("http://127.0.0.1:18081/d/v2demo?viewPanel=20", "vendor-gauge-repro.txt")
    assert "Supermicro" in gauge and "Dell Inc." in gauge and "Lenovo" in gauge
    assert "Value" not in gauge

    vendor_table = run_tui("http://127.0.0.1:18081/d/v2demo?viewPanel=21", "vendor-table-repro.txt")
    assert "cluster" in vendor_table and "model" in vendor_table and "vendor" in vendor_table and "nodes" in vendor_table
    assert "alpha" in vendor_table and "PowerEdge R6715" in vendor_table and "Dell Inc." in vendor_table
    assert re.search(r"cluster\s+model\s+vendor\s+nodes", vendor_table)

    geohash = run_tui("http://127.0.0.1:18081/d/v2demo?viewPanel=22", "geohash-repro.txt")
    assert re.search(r"Copenhagen\s+57\.6491\s+10\.4074\s+20", geohash)

    repeated_tabs = run_tui("http://127.0.0.1:18081/d/v2repeattabs", "v2-repeat-tabs-repro.txt")
    assert "## alpha" in repeated_tabs and "## beta" in repeated_tabs
    assert "$cluster" not in repeated_tabs
    static_tab = run_tui("http://127.0.0.1:18081/d/v2repeattabs?viewPanel=24", "v2-static-tab-repro.txt")
    assert "Static" in static_tab
    print("v2 AutoGrid, stacked/repeated Tabs, library panels, geomap/geohash, vendor barchart, bar gauge, and vendor table: OK")
finally:
    server.terminate()
    server.wait(timeout=5)
