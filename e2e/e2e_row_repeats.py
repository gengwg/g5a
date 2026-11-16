#!/usr/bin/env python3
import datetime
import json
import os
import subprocess
import sys

from harness import artifact, fixture_path, run_tui, wait_for_server

server = subprocess.Popen([sys.executable, str(fixture_path("row_repeat_server.py"))])
try:
    wait_for_server("http://127.0.0.1:18082/api/dashboards/uid/rowrepeat")

    for uid in ("rowrepeat", "v2rowrepeat"):
        output = run_tui(
            f"http://127.0.0.1:18082/d/{uid}",
            f"{uid}-repro.txt",
        )
        for expected in ("alpha", "beta", "Following", "11", "22", "99"):
            assert expected in output, (uid, expected, output)
        assert "unsupported" not in output.lower(), (uid, output)
        print(f"{uid}: repeated rows, scoped queries, and following panel: OK")

    switch = run_tui(
        "http://127.0.0.1:18082/d/v2switch",
        "v2-switch-repro.txt",
    )
    assert "on" in switch and "42" in switch and "$switch" not in switch
    print("v2 switch variable selection and query interpolation: OK")

    weather = run_tui(
        "http://127.0.0.1:18082/d/weatherunits",
        "weather-units-repro.txt",
    )
    assert "11.3 °C" in weather and "100.8 m/s" in weather
    assert "celsius" not in weather and "velocityms" not in weather
    print("weather temperature and velocity units: OK")

    versions = run_tui(
        "http://127.0.0.1:18082/d/versionskew",
        "version-skew-repro.txt",
    )
    assert "GPU driver" in versions and "CUDA driver" in versions
    assert "Time 1" not in versions and "Time #" not in versions and "2023-" not in versions
    print("version-skew table excludes duplicate time fields: OK")

    workload = run_tui(
        "http://127.0.0.1:18082/d/workloadparity",
        "workload-parity-repro.txt",
    )
    for expected in ("CPU req (cores)", "Mem req", "GPU req", "340,024"):
        assert expected in workload, (expected, workload)
    assert "namespace_workload:" not in workload
    assert " locale" not in workload
    print("eight-query merge and locale formatting: OK")

    capacity = run_tui(
        "http://127.0.0.1:18082/d/capacityparity",
        "capacity-parity-repro.txt",
    )
    for expected in ("Nodes", "CPU cores", "GPU nodes", "GPUs (allocatable)", "GPUs (DCGM)"):
        assert expected in capacity, (expected, capacity)
    for excluded in ("Time 2", "Time 3", "Time 4", "Time 5", "2023-"):
        assert excluded not in capacity, (excluded, capacity)
    print("capacity table coalesces time fields by refID: OK")

    alerts = run_tui(
        "http://127.0.0.1:18082/d/alerthealth",
        "alert-health-repro.txt",
    )
    assert "No data health" in alerts and "NO_DATA" in alerts
    assert "Error health" not in alerts
    print("No Data alert rule health and filtering: OK")

    all_selection = run_tui(
        "http://127.0.0.1:18082/d/allselection",
        "all-selection-repro.txt",
    )
    assert "42" in all_selection and "99" not in all_selection
    assert ".*" not in all_selection
    print("All selection uses real option values: OK")

    run_tui(
        "http://127.0.0.1:18082/d/rowrepeat?from=now-6h&to=now&var-cluster=alpha&orgId=2",
        "url-options-repro.txt",
    )
    with open(artifact("last-queries.jsonl")) as capture:
        requests = [json.loads(line) for line in capture]
    assert any(request["_orgId"] == "2" and any(query.get("expr") == "alpha" for query in request["queries"]) for request in requests)
    assert any(int(request["to"]) - int(request["from"]) == 6 * 60 * 60 * 1000 for request in requests)
    print("URL time, variable, and org options: OK")

    timezone_output = run_tui(
        "http://127.0.0.1:18082/d/timezone",
        "timezone-repro.txt",
    )
    with open(artifact("last-queries.jsonl")) as capture:
        requests = [json.loads(line) for line in capture]
    assert any(int(request["to"]) - int(request["from"]) == 24 * 60 * 60 * 1000 - 1 for request in requests)
    expected_time = datetime.datetime.fromtimestamp(1700000000, datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
    assert expected_time in timezone_output
    print("dashboard timezone and UTC timestamps: OK")

    if os.path.exists(artifact("label-values-request.json")):
        os.remove(artifact("label-values-request.json"))
    never = run_tui("http://127.0.0.1:18082/d/refreshnever", "refresh-never-repro.txt")
    assert "42" in never and "99" not in never
    assert not os.path.exists(artifact("label-values-request.json"))
    load = run_tui("http://127.0.0.1:18082/d/refreshload", "refresh-load-repro.txt")
    assert "dev" in load and "99" in load
    with open(artifact("label-values-request.json")) as capture:
        label_request = json.load(capture)
    assert label_request["start"] and label_request["end"]
    print("variable refresh mode and label_values time range: OK")

    run_tui("http://127.0.0.1:18082/d/refreshload", "refresh-time-change-repro.txt", keys="tkk\r")
    with open(artifact("last-queries.jsonl")) as capture:
        requests = [json.loads(line) for line in capture]
    assert all("label_values(" not in query.get("expr", "") for request in requests for query in request["queries"])
    assert any(query.get("expr") == "dev" for request in requests for query in request["queries"])
    print("time change preserves resolved variable options: OK")

    run_tui("http://127.0.0.1:18082/d/intervals", "intervals-repro.txt")
    with open(artifact("last-queries.jsonl")) as capture:
        requests = [json.loads(line) for line in capture]
    assert any(query.get("expr") == "rate(a[20m])" and query.get("maxDataPoints") == 42 for request in requests for query in request["queries"])
    assert any(query.get("expr") == "rate(b[4m])" for request in requests for query in request["queries"])
    print("min step and datasource scrape interval: OK")

    run_tui("http://127.0.0.1:18082/d/builtins", "builtins-repro.txt")
    with open(artifact("last-queries.jsonl")) as capture:
        requests = [json.loads(line) for line in capture]
    found = False
    for request in requests:
        for query in request["queries"]:
            if query.get("expr", "").count("|") == 3:
                interval, rate_ms, from_ms, to_ms = query["expr"].split("|")
                assert interval.endswith("ms") and rate_ms.isdigit()
                assert from_ms == request["from"] and to_ms == request["to"]
                found = True
    assert found
    print("braced globals, rate interval ms, and epoch from/to: OK")

    query_result = run_tui(
        "http://127.0.0.1:18082/d/queryresult",
        "query-result-repro.txt",
    )
    assert "11" in query_result and "99" not in query_result
    assert "Error:" not in query_result
    print("query_result metric, value, and timestamp sample text: OK")

    transformations = run_tui(
        "http://127.0.0.1:18082/d/transformfidelity",
        "transformation-fidelity-repro.txt",
    )
    assert all(text in transformations for text in ("First", "Last", "Max", "temperature", "humidity"))
    assert "boundary" in transformations and "middle" in transformations
    assert "outside" not in transformations
    inner = transformations.split("Inner join", 1)[1].split("Outer tabular", 1)[0]
    assert "shared" in inner and "leftOnly" not in inner and "rightOnly" not in inner
    outer = transformations.split("Outer tabular", 1)[1]
    assert all(text in outer for text in ("shared", "leftOnly", "rightOnly"))
    assert "unsupported" not in transformations
    print("reducers, time filters, and inner/outer-tabular joins: OK")

    grouped_time = run_tui(
        "http://127.0.0.1:18082/d/grouptime",
        "group-time-repro.txt",
    )
    assert "Merged" in grouped_time and "MR (count)" in grouped_time
    dates = ["2023-11-15", "2023-11-14", "2023-11-16"]
    positions = [grouped_time.index(date) for date in dates]
    assert positions == sorted(positions), grouped_time
    assert "3" in grouped_time and "2" in grouped_time and "0" in grouped_time
    assert "unsupported" not in grouped_time
    print("formatTime, groupBy, and sortBy terminal pipeline: OK")
finally:
    server.terminate()
    server.wait(timeout=5)
