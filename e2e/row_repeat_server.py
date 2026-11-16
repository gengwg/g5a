import copy
import json
import threading
import urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from harness import artifact
from repro_matrix_server import frame, v2_layout_item, v2_panel

capture_lock = threading.Lock()

PANEL = {
    "id": 30, "type": "stat", "title": "$cluster",
    "datasource": {"uid": "prom1", "type": "prometheus"},
    "targets": [{
        "refId": "K", "expr": "$cluster", "instant": True, "range": False,
        "datasource": {"uid": "prom1", "type": "prometheus"},
    }],
    "gridPos": {"x": 0, "y": 1, "w": 24, "h": 4},
}
FOLLOWING = {
    "id": 32, "type": "stat", "title": "Following",
    "datasource": {"uid": "prom1", "type": "prometheus"},
    "targets": [{
        "refId": "K", "expr": "following", "instant": True, "range": False,
        "datasource": {"uid": "prom1", "type": "prometheus"},
    }],
    "gridPos": {"x": 0, "y": 6, "w": 24, "h": 4},
}
CLASSIC = {
    "title": "Classic row repeat", "refresh": False,
    "time": {"from": "now-1h", "to": "now"},
    "templating": {"list": [{
        "name": "cluster", "type": "custom", "query": "alpha,beta",
        "multi": True, "includeAll": True,
        "current": {"text": "All", "value": "$__all"},
    }]},
    "panels": [
        {"id": 29, "type": "row", "title": "$cluster", "repeat": "cluster",
         "collapsed": False, "panels": [], "gridPos": {"x": 0, "y": 0, "w": 24, "h": 1}},
        PANEL,
        {"id": 31, "type": "row", "title": "Next", "collapsed": False,
         "panels": [], "gridPos": {"x": 0, "y": 5, "w": 24, "h": 1}},
        FOLLOWING,
    ],
}
V2 = {
    "apiVersion": "dashboard.grafana.app/v2",
    "spec": {
        "title": "V2 row repeat",
        "timeSettings": {"from": "now-1h", "to": "now", "autoRefresh": False},
        "elements": {
            "panel-30": v2_panel(PANEL),
            "panel-32": v2_panel(FOLLOWING),
        },
        "layout": {"kind": "RowsLayout", "spec": {"rows": [
            {"kind": "RowsLayoutRow", "spec": {
                "title": "$cluster",
                "repeat": {"mode": "variable", "value": "cluster"},
                "layout": {"kind": "GridLayout", "spec": {"items": [v2_layout_item(PANEL)]}},
            }},
            {"kind": "RowsLayoutRow", "spec": {
                "title": "Next",
                "layout": {"kind": "GridLayout", "spec": {"items": [v2_layout_item(FOLLOWING)]}},
            }},
        ]}},
        "variables": [{"kind": "CustomVariable", "spec": {
            "name": "cluster", "query": "alpha,beta",
            "current": {"text": ["All"], "value": ["$__all"]},
            "options": [
                {"text": ["alpha"], "value": ["alpha"]},
                {"text": ["beta"], "value": ["beta"]},
            ],
            "multi": True, "includeAll": True,
        }}],
    },
}


SWITCH_PANEL = copy.deepcopy(PANEL)
SWITCH_PANEL["title"] = "$switch"
SWITCH_PANEL["targets"][0]["expr"] = "$switch"
V2_SWITCH = {
    "apiVersion": "dashboard.grafana.app/v2",
    "spec": {
        "title": "V2 switch",
        "timeSettings": {"from": "now-1h", "to": "now", "autoRefresh": False},
        "elements": {"panel-30": v2_panel(SWITCH_PANEL)},
        "layout": {"kind": "GridLayout", "spec": {"items": [v2_layout_item(SWITCH_PANEL)]}},
        "variables": [{"kind": "SwitchVariable", "spec": {
            "name": "switch", "current": "on",
            "enabledValue": "on", "disabledValue": "off",
        }}],
    },
}


TEMP_PANEL = copy.deepcopy(PANEL)
TEMP_PANEL.update({
    "id": 40, "title": "Outdoor temperature",
    "fieldConfig": {"defaults": {"unit": "celsius"}},
})
TEMP_PANEL["targets"][0]["expr"] = "temperature"
WIND_PANEL = copy.deepcopy(PANEL)
WIND_PANEL.update({
    "id": 41, "title": "Wind gusts",
    "fieldConfig": {"defaults": {"unit": "velocityms"}},
    "gridPos": {"x": 0, "y": 5, "w": 24, "h": 4},
})
WIND_PANEL["targets"][0]["expr"] = "wind"
WEATHER_UNITS = {
    "title": "Weather units", "refresh": False,
    "time": {"from": "now-1h", "to": "now"},
    "templating": {"list": []},
    "panels": [TEMP_PANEL, WIND_PANEL],
}


VERSION_SKEW = copy.deepcopy(PANEL)
VERSION_SKEW.update({
    "id": 42, "type": "table", "title": "Distinct versions per cluster",
    "targets": [
        {"refId": "A", "expr": "versionA", "instant": True, "range": False,
         "datasource": {"uid": "prom1", "type": "prometheus"}},
        {"refId": "B", "expr": "versionB", "instant": True, "range": False,
         "datasource": {"uid": "prom1", "type": "prometheus"}},
    ],
    "options": {"showHeader": True},
    "transformations": [
        {"id": "joinByField", "options": {"byField": "cluster", "mode": "outer"}},
        {"id": "organize", "options": {
            "excludeByName": {"Time": True, "Time 1": True},
            "renameByName": {"Value #A": "GPU driver", "Value #B": "CUDA driver", "cluster": "Cluster"},
        }},
    ],
})
VERSION_SKEW = {
    "title": "Version skew", "refresh": False,
    "time": {"from": "now-1h", "to": "now"},
    "templating": {"list": []},
    "panels": [VERSION_SKEW],
}


WORKLOAD_PANEL = copy.deepcopy(PANEL)
WORKLOAD_PANEL.update({
    "id": 43, "type": "table", "title": "Workload requests and limits",
    "targets": [],
    "options": {"showHeader": True},
    "transformations": [
        {"id": "merge", "options": {}},
        {"id": "organize", "options": {
            "excludeByName": {"Time": True, "cluster": True},
            "renameByName": {
                "Value #A": "CPU req (cores)", "Value #B": "CPU p95 (cores)",
                "Value #C": "CPU limit (cores)", "Value #D": "CPU util",
                "Value #E": "Mem req", "Value #F": "Mem p95",
                "Value #G": "Mem util", "Value #H": "GPU req",
            },
        }},
    ],
})
for ref in "ABCDEFGH":
    WORKLOAD_PANEL["targets"].append({
        "refId": ref, "expr": ref, "instant": True, "range": False,
        "datasource": {"uid": "prom1", "type": "prometheus"},
    })
LOCALE_PANEL = copy.deepcopy(PANEL)
LOCALE_PANEL.update({
    "id": 44, "title": "Total GPUs",
    "fieldConfig": {"defaults": {"unit": "locale"}},
    "gridPos": {"x": 0, "y": 15, "w": 24, "h": 4},
})
LOCALE_PANEL["targets"][0]["expr"] = "capacity"
WORKLOAD_DASHBOARD = {
    "title": "Workload parity", "refresh": False,
    "time": {"from": "now-1h", "to": "now"},
    "templating": {"list": []},
    "panels": [WORKLOAD_PANEL, LOCALE_PANEL],
}


CAPACITY_PANEL = copy.deepcopy(PANEL)
CAPACITY_PANEL.update({
    "id": 45, "type": "table", "title": "Capacity by cluster",
    "targets": [],
    "options": {"showHeader": True},
    "transformations": [
        {"id": "joinByField", "options": {"byField": "cluster", "mode": "outer"}},
        {"id": "organize", "options": {
            "excludeByName": {
                "Time": True, "Time 1": True, "Time 2": True,
                "Time 3": True, "Time 4": True, "Time 5": True,
            },
            "renameByName": {
                "Value #A": "Nodes", "Value #B": "CPU cores",
                "Value #C": "GPU nodes", "Value #D": "GPUs (allocatable)",
                "Value #E": "GPUs (DCGM)", "cluster": "Cluster",
            },
        }},
    ],
})
for ref in "ABCDE":
    CAPACITY_PANEL["targets"].append({
        "refId": ref, "expr": ref, "instant": True, "range": False,
        "datasource": {"uid": "prom1", "type": "prometheus"},
    })
CAPACITY_DASHBOARD = {
    "title": "Capacity parity", "refresh": False,
    "time": {"from": "now-1h", "to": "now"},
    "templating": {"list": []},
    "panels": [CAPACITY_PANEL],
}


ALERT_HEALTH_DASHBOARD = {
    "title": "Alert health", "refresh": False,
    "time": {"from": "now-1h", "to": "now"},
    "templating": {"list": []},
    "panels": [{
        "id": 46, "type": "alertlist", "title": "No data rules",
        "gridPos": {"x": 0, "y": 0, "w": 24, "h": 8},
        "options": {"viewMode": "list", "stateFilter": {"noData": True}},
    }],
}


ALL_DASHBOARD = {
    "title": "All interpolation", "refresh": False,
    "time": {"from": "now-1h", "to": "now"},
    "templating": {"list": [{
        "name": "cluster", "type": "custom", "query": "alpha,beta",
        "multi": True, "includeAll": True,
        "current": {"text": "All", "value": "$__all"},
    }]},
    "panels": [{
        "id": 47, "type": "stat", "title": "All selection",
        "datasource": {"uid": "prom1", "type": "prometheus"},
        "targets": [{
            "refId": "K", "expr": "$cluster", "instant": True, "range": False,
            "datasource": {"uid": "prom1", "type": "prometheus"},
        }],
        "gridPos": {"x": 0, "y": 0, "w": 24, "h": 4},
    }],
}


TIMEZONE_DASHBOARD = {
    "title": "Timezone", "refresh": False, "timezone": "UTC", "weekStart": "monday",
    "time": {"from": "now/d", "to": "now/d"},
    "templating": {"list": []},
    "panels": [{
        "id": 48, "type": "table", "title": "UTC table",
        "datasource": {"uid": "prom1", "type": "prometheus"},
        "targets": [{
            "refId": "K", "expr": "time", "instant": True, "range": False,
            "datasource": {"uid": "prom1", "type": "prometheus"},
        }],
        "gridPos": {"x": 0, "y": 0, "w": 24, "h": 5},
    }],
}


def refresh_dashboard(refresh):
    return {
        "title": "Variable refresh", "refresh": False,
        "time": {"from": "now-1h", "to": "now"},
        "templating": {"list": [{
            "name": "cluster", "type": "query", "refresh": refresh,
            "datasource": {"uid": "prom1", "type": "prometheus"},
            "definition": "label_values(up, cluster)",
            "current": {"value": "prod"},
            "options": [
                {"text": "prod", "value": "prod"},
                {"text": "dev", "value": "dev"},
            ],
        }]},
        "panels": [{
            "id": 49, "type": "stat", "title": "$cluster",
            "datasource": {"uid": "prom1", "type": "prometheus"},
            "targets": [{
                "refId": "K", "expr": "$cluster", "instant": True, "range": False,
                "datasource": {"uid": "prom1", "type": "prometheus"},
            }],
            "gridPos": {"x": 0, "y": 0, "w": 24, "h": 4},
        }],
    }


REFRESH_NEVER_DASHBOARD = refresh_dashboard(0)
REFRESH_LOAD_DASHBOARD = refresh_dashboard(1)


INTERVAL_DASHBOARD = {
    "title": "Intervals", "refresh": False,
    "time": {"from": "now-1h", "to": "now"},
    "templating": {"list": []},
    "panels": [{
        "id": 50, "type": "timeseries", "title": "Intervals",
        "datasource": {"uid": "prom1", "type": "prometheus"},
        "targets": [
            {"refId": "A", "expr": "rate(a[$__rate_interval])", "interval": "5m", "maxDataPoints": 42,
             "instant": False, "range": True, "datasource": {"uid": "prom1", "type": "prometheus"}},
            {"refId": "B", "expr": "rate(b[$__rate_interval])",
             "instant": False, "range": True, "datasource": {"uid": "prom1", "type": "prometheus"}},
        ],
        "gridPos": {"x": 0, "y": 0, "w": 24, "h": 5},
    }],
}


BUILTINS_DASHBOARD = {
    "title": "Builtins", "refresh": False,
    "time": {"from": "now-1h", "to": "now"},
    "templating": {"list": []},
    "panels": [{
        "id": 51, "type": "timeseries", "title": "Builtins",
        "datasource": {"uid": "prom1", "type": "prometheus"},
        "targets": [{
            "refId": "A", "expr": "${__interval}|${__rate_interval_ms}|${__from}|${__to}",
            "instant": False, "range": True,
            "datasource": {"uid": "prom1", "type": "prometheus"},
        }],
        "gridPos": {"x": 0, "y": 0, "w": 24, "h": 5},
    }],
}


def query_result_frame():
    return {
        "schema": {"refId": "vars", "fields": [
            {"name": "Time", "type": "time"},
            {"name": "up", "type": "number", "labels": {"cluster": "alpha"}},
        ]},
        "data": {"values": [[1000], [1]]},
    }


QUERY_RESULT_DASHBOARD = {
    "title": "Query result", "refresh": False,
    "time": {"from": "now-1h", "to": "now"},
    "templating": {"list": [{
        "name": "cluster", "type": "query", "refresh": 1, "multi": True, "includeAll": True,
        "datasource": {"uid": "prom1", "type": "prometheus"},
        "query": "query_result(up{cluster=\"alpha\"})",
        "regex": "/^up\\{cluster=\"([^\"]+)\"\\} 1 1000$/",
        "current": {"value": "alpha"},
        "options": [{"text": "alpha", "value": "alpha"}],
    }]},
    "panels": [{
        "id": 52, "type": "stat", "title": "$cluster",
        "datasource": {"uid": "prom1", "type": "prometheus"},
        "targets": [{
            "refId": "K", "expr": "$cluster", "instant": True, "range": False,
            "datasource": {"uid": "prom1", "type": "prometheus"},
        }],
        "gridPos": {"x": 0, "y": 0, "w": 24, "h": 4},
    }],
}


TRANSFORM_DASHBOARD = {
    "title": "Transformation fidelity", "refresh": False, "timezone": "UTC",
    "time": {"from": "1000", "to": "2000"},
    "templating": {"list": []},
    "panels": [
        {
            "id": 53, "type": "table", "title": "Reduced",
            "datasource": {"uid": "prom1", "type": "prometheus"},
            "targets": [{"refId": "K", "expr": "reduce", "instant": True, "range": False,
                         "datasource": {"uid": "prom1", "type": "prometheus"}}],
            "transformations": [{"id": "reduce", "options": {
                "mode": "seriesToRows", "reducers": ["first", "last", "max"],
            }}],
            "gridPos": {"x": 0, "y": 0, "w": 12, "h": 6},
        },
        {
            "id": 54, "type": "table", "title": "Time filtered",
            "datasource": {"uid": "prom1", "type": "prometheus"},
            "targets": [{"refId": "K", "expr": "timefilter", "instant": True, "range": False,
                         "datasource": {"uid": "prom1", "type": "prometheus"}}],
            "transformations": [{"id": "filterByValue", "options": {
                "type": "include", "match": "all",
                "filters": [{"fieldName": "Time", "config": {"id": "between", "options": {
                    "from": "$__from", "to": "${__to}", "inclusive": True,
                }}}],
            }}],
            "gridPos": {"x": 12, "y": 0, "w": 12, "h": 6},
        },
        {
            "id": 55, "type": "stat", "title": "Unsupported join",
            "datasource": {"uid": "prom1", "type": "prometheus"},
            "targets": [{"refId": "K", "expr": "join", "instant": True, "range": False,
                         "datasource": {"uid": "prom1", "type": "prometheus"}}],
            "transformations": [{"id": "joinByField", "options": {"byField": "Time", "mode": "inner"}}],
            "gridPos": {"x": 0, "y": 6, "w": 24, "h": 4},
        },
    ],
}


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/api/dashboards/uid/rowrepeat":
            self.send_json({"dashboard": CLASSIC})
        elif self.path == "/api/dashboards/uid/weatherunits":
            self.send_json({"dashboard": WEATHER_UNITS})
        elif self.path == "/api/dashboards/uid/versionskew":
            self.send_json({"dashboard": VERSION_SKEW})
        elif self.path == "/api/dashboards/uid/workloadparity":
            self.send_json({"dashboard": WORKLOAD_DASHBOARD})
        elif self.path == "/api/dashboards/uid/capacityparity":
            self.send_json({"dashboard": CAPACITY_DASHBOARD})
        elif self.path == "/api/dashboards/uid/alerthealth":
            self.send_json({"dashboard": ALERT_HEALTH_DASHBOARD})
        elif self.path == "/api/dashboards/uid/allselection":
            self.send_json({"dashboard": ALL_DASHBOARD})
        elif self.path == "/api/dashboards/uid/timezone":
            self.send_json({"dashboard": TIMEZONE_DASHBOARD})
        elif self.path == "/api/dashboards/uid/refreshnever":
            self.send_json({"dashboard": REFRESH_NEVER_DASHBOARD})
        elif self.path == "/api/dashboards/uid/refreshload":
            self.send_json({"dashboard": REFRESH_LOAD_DASHBOARD})
        elif self.path == "/api/dashboards/uid/intervals":
            self.send_json({"dashboard": INTERVAL_DASHBOARD})
        elif self.path == "/api/dashboards/uid/builtins":
            self.send_json({"dashboard": BUILTINS_DASHBOARD})
        elif self.path == "/api/dashboards/uid/queryresult":
            self.send_json({"dashboard": QUERY_RESULT_DASHBOARD})
        elif self.path == "/api/dashboards/uid/transformfidelity":
            self.send_json({"dashboard": TRANSFORM_DASHBOARD})
        elif self.path.startswith("/api/datasources/proxy/uid/prom1/api/v1/label/cluster/values"):
            query = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)
            with capture_lock:
                with open(artifact("label-values-request.json"), "w") as capture:
                    json.dump({"start": query.get("start", [""])[0], "end": query.get("end", [""])[0]}, capture)
            self.send_json({"status": "success", "data": ["dev"]})
        elif self.path == "/api/prometheus/grafana/api/v1/rules":
            self.send_json({"data": {"groups": [{
                "name": "g", "file": "f", "rules": [
                    {"name": "No data health", "state": "inactive", "health": "nodata", "type": "alerting", "annotations": {}, "alerts": []},
                    {"name": "Error health", "state": "inactive", "health": "error", "type": "alerting", "annotations": {}, "alerts": []},
                ],
            }]}})
        elif self.path == "/apis/dashboard.grafana.app/v2/namespaces/default/dashboards/v2rowrepeat":
            self.send_json(V2)
        elif self.path == "/apis/dashboard.grafana.app/v2/namespaces/default/dashboards/v2switch":
            self.send_json(V2_SWITCH)
        elif self.path == "/api/datasources":
            self.send_json([{"uid": "prom1", "name": "Prometheus", "type": "prometheus", "jsonData": {"timeInterval": "60s"}}])
        elif self.path.startswith("/api/search"):
            self.send_json([{"uid": "rowrepeat", "title": "Classic row repeat", "type": "dash-db"}])
        else:
            self.send_error(404)

    def do_POST(self):
        if self.path != "/api/ds/query":
            self.send_error(404)
            return
        payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        payload["_orgId"] = self.headers.get("X-Grafana-Org-Id")
        with capture_lock:
            with open(artifact("last-queries.jsonl"), "a") as capture:
                capture.write(json.dumps(payload) + "\n")
        results = {}
        for query in payload["queries"]:
            if query["refId"] == "vars":
                results["vars"] = {"frames": [query_result_frame()]}
                continue
            cluster = query.get("expr", "")
            if cluster == "reduce":
                results[query["refId"]] = {"frames": [{
                    "schema": {"refId": query["refId"], "fields": [
                        {"name": "temperature", "type": "number"},
                        {"name": "humidity", "type": "number"},
                    ]},
                    "data": {"values": [[3, 4, 5, 6], [10, 20, 30, 40]]},
                }]}
                continue
            if cluster == "timefilter":
                results[query["refId"]] = {"frames": [{
                    "schema": {"refId": query["refId"], "fields": [
                        {"name": "Key", "type": "string"},
                        {"name": "Time", "type": "time"},
                    ]},
                    "data": {"values": [["boundary", "middle", "outside"], [1000, 1500, 2001]]},
                }]}
                continue
            if cluster in ("versionA", "versionB"):
                cluster = "alpha"
            value = {"alpha": 11, "beta": 22, "on": 42, "prod": 42, "dev": 99, "(alpha|beta)": 42, "temperature": 11.3, "wind": 100.8, "capacity": 340024}.get(cluster, 99)
            results[query["refId"]] = {"frames": [frame(query["refId"], value, cluster)]}
        self.send_json({"results": results})

    def send_json(self, value):
        body = json.dumps(value).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_):
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", 18082), Handler).serve_forever()
