#!/usr/bin/env python3
import copy
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

TRANSFORMS = [
    {"id": "joinByField", "options": {"byField": "cluster", "mode": "outer"}},
    {"id": "filterFieldsByName", "options": {"include": {"pattern": "^(cluster|Value #.*)$"}}},
    {"id": "organize", "options": {
        "indexByName": {"cluster": 0, "Value #A": 1, "Value #B": 2, "Value #C": 3, "Value #D": 4},
        "renameByName": {"Value #A": "NotReady", "Value #B": "Cordoned, Ready", "Value #C": "Not allocatable, Ready", "Value #D": "Total"},
    }},
    {"id": "sortBy", "options": {"sort": [{"field": "Total", "desc": True}]}},
]

TABLE_SERIES_PANEL = {
    "id": 17, "type": "table", "title": "Current GPU values",
    "datasource": {"uid": "${datasource}", "type": "prometheus"},
    "targets": [
        {"refId": ref, "expr": ref, "datasource": {"uid": "${datasource}", "type": "prometheus"}, "instant": True, "range": False}
        for ref in ("G", "H")
    ],
    "gridPos": {"x": 0, "y": 34, "w": 24, "h": 8},
    "options": {"showHeader": True},
}

GEO_PANEL = {
    "id": 18, "type": "geomap", "title": "Site temperatures",
    "datasource": {"uid": "${datasource}", "type": "prometheus"},
    "targets": [{"refId": "I", "expr": "I", "datasource": {"uid": "${datasource}", "type": "prometheus"}, "instant": True, "range": False}],
    "gridPos": {"x": 0, "y": 42, "w": 24, "h": 10},
}

VENDOR_PANEL = {
    "id": 19, "type": "barchart", "title": "Nodes by vendor",
    "datasource": {"uid": "${datasource}", "type": "prometheus"},
    "targets": [{"refId": "J", "expr": "J", "datasource": {"uid": "${datasource}", "type": "prometheus"}, "instant": True, "range": False}],
    "gridPos": {"x": 0, "y": 52, "w": 24, "h": 10},
}

VENDOR_GAUGE_PANEL = copy.deepcopy(VENDOR_PANEL)
VENDOR_GAUGE_PANEL.update({
    "id": 20, "type": "bargauge", "title": "Nodes by vendor gauge",
    "gridPos": {"x": 0, "y": 62, "w": 24, "h": 10},
})

VENDOR_TABLE_PANEL = {
    "id": 21, "type": "table", "title": "Node vendor / model per cluster",
    "datasource": {"uid": "${datasource}", "type": "prometheus"},
    "targets": [{"refId": "K", "expr": "K", "datasource": {"uid": "${datasource}", "type": "prometheus"}, "instant": True, "range": False}],
    "gridPos": {"x": 0, "y": 72, "w": 24, "h": 8},
    "options": {"showHeader": True, "footer": {"show": False}},
    "transformations": [{"id": "organize", "options": {
        "excludeByName": {"Time": True},
        "renameByName": {"Value": "nodes", "product_name": "model", "system_vendor": "vendor"},
    }}],
}

GEOHASH_PANEL = {
    "id": 22, "type": "geomap", "title": "Geohash site",
    "datasource": {"uid": "${datasource}", "type": "prometheus"},
    "targets": [{"refId": "L", "expr": "L", "datasource": {"uid": "${datasource}", "type": "prometheus"}, "instant": True, "range": False}],
    "gridPos": {"x": 0, "y": 80, "w": 24, "h": 10},
}

ALERT_GROUP_PANEL = {
    "id": 16, "type": "alertlist", "title": "Alerts by severity",
    "gridPos": {"x": 0, "y": 26, "w": 24, "h": 8},
    "options": {
        "viewMode": "list", "groupMode": "custom", "groupBy": ["severity"],
        "maxItems": 20, "showInstances": True,
        "dashboardAlerts": True, "stateFilter": {"firing": True},
    },
}

ALERT_PANEL = {
    "id": 15, "type": "alertlist", "title": "Current alerts",
    "gridPos": {"x": 0, "y": 18, "w": 24, "h": 8},
    "options": {
        "viewMode": "list", "maxItems": 20, "showInstances": True,
        "dashboardAlerts": True, "stateFilter": {"firing": True},
    },
}

SLO_PANEL = {
    "id": 14, "type": "stat", "title": "Budget left",
    "datasource": {"uid": "${datasource}", "type": "prometheus"},
    "targets": [
        {"refId": ref, "expr": ref, "datasource": {"uid": "${datasource}", "type": "prometheus"}, "instant": True, "range": False}
        for ref in ("E", "F")
    ],
    "gridPos": {"x": 0, "y": 12, "w": 24, "h": 6},
    "fieldConfig": {"defaults": {"unit": "percentunit", "decimals": 2}},
    "transformations": [
        {"id": "joinByField", "options": {"byField": "Time", "mode": "outer"}},
        {"id": "calculateField", "options": {
            "mode": "binary", "alias": "Budget left", "replaceFields": True,
            "binary": {"left": "Value #E", "operator": "-", "right": "Value #F"},
        }},
    ],
}

PANEL = {
    "id": 13, "type": "table", "title": "GPU-hours unavailable",
    "datasource": {"uid": "${datasource}", "type": "prometheus"},
    "targets": [
        {"refId": ref, "expr": ref, "datasource": {"uid": "${datasource}", "type": "prometheus"}, "instant": True, "range": False, "format": "table"}
        for ref in ("A", "B", "C", "D")
    ],
    "gridPos": {"x": 0, "y": 0, "w": 24, "h": 12},
    "fieldConfig": {"defaults": {"noValue": "-", "unit": "suffix: GPU-h", "decimals": 0}},
    "options": {
        "showHeader": True, "sortBy": [{"displayName": "Total", "desc": True}],
        "footer": {"show": True, "reducer": ["sum"], "fields": ""},
    },
    "transformations": TRANSFORMS,
}

DASHBOARD = {
    "title": "Matrix Reproduction", "refresh": False, "time": {"from": "now-1h", "to": "now"},
    "templating": {"list": [{"name": "datasource", "type": "datasource", "query": "prometheus", "current": {"value": "prom1"}}]},
    "panels": [PANEL, SLO_PANEL, ALERT_PANEL, ALERT_GROUP_PANEL, TABLE_SERIES_PANEL, GEO_PANEL, VENDOR_PANEL, VENDOR_GAUGE_PANEL, VENDOR_TABLE_PANEL, GEOHASH_PANEL],
}


def geo_frame():
    return {
        "schema": {"refId": "I", "fields": [
            {"name": "site", "type": "string"},
            {"name": "latitude", "type": "number"},
            {"name": "longitude", "type": "number"},
            {"name": "temperature", "type": "number"},
        ]},
        "data": {"values": [
            ["San Francisco", "Berlin"],
            [37.7749, 52.52],
            [-122.4194, 13.405],
            [18, 22],
        ]},
    }


def vendor_frame(vendor, value):
    return {
        "schema": {"refId": "J", "fields": [
            {"name": "Time", "type": "time"},
            {"name": "Value", "type": "number", "labels": {"vendor": vendor}},
        ]},
        "data": {"values": [[1700000000000], [value]]},
    }


def vendor_model_frame():
    return {
        "schema": {"refId": "K", "fields": [
            {"name": "Time", "type": "time"},
            {"name": "Value", "type": "number", "labels": {
                "cluster": "alpha", "product_name": "PowerEdge R6715", "system_vendor": "Dell Inc.",
            }},
        ]},
        "data": {"values": [[1700000000000], [5]]},
    }


def geohash_frame():
    return {
        "schema": {"refId": "L", "fields": [
            {"name": "site", "type": "string"},
            {"name": "geohash", "type": "string"},
            {"name": "temperature", "type": "number"},
        ]},
        "data": {"values": [["Copenhagen"], ["u4pruydqqvj"], [20]]},
    }


def frame(ref, value, cluster):
    return {
        "schema": {"refId": ref, "fields": [
            {"name": "Time", "type": "time", "config": {"interval": 30000}},
            {"name": "Value", "type": "number", "labels": {"cluster": cluster}, "config": {"displayNameFromDS": '{cluster="' + cluster + '"}'}},
        ]},
        "data": {"values": [[1700000000000], [value]]},
    }


def v2_panel(panel):
    return {
        "kind": "Panel",
        "spec": {
            "id": panel["id"],
            "title": panel["title"],
            "vizConfig": {
                "kind": "VizConfig", "group": panel["type"], "version": "1.0",
                "spec": {
                    "fieldConfig": panel.get("fieldConfig", {}),
                    "options": panel.get("options", {}),
                },
            },
            "data": {
                "kind": "QueryGroup",
                "spec": {
                    "queries": [
                        {
                            "kind": "PanelQuery",
                            "spec": {
                                "refId": target["refId"], "hidden": target.get("hide", False),
                                "query": {
                                    "kind": "DataQuery", "group": "prometheus",
                                    "datasource": {"name": target["datasource"]["uid"]},
                                    "spec": {key: value for key, value in target.items() if key not in ("refId", "hide", "datasource")},
                                },
                            },
                        }
                        for target in panel.get("targets", [])
                    ],
                    "transformations": [
                        {"kind": "Transformation", "group": transform["id"],
                         "spec": {"options": transform.get("options", {})}}
                        for transform in panel.get("transformations", [])
                    ],
                },
            },
        },
    }


def v2_layout_item(panel):
    position = panel["gridPos"]
    return {
        "kind": "GridLayoutItem",
        "spec": {
            "x": position["x"], "y": position["y"],
            "width": position["w"], "height": position["h"],
            "element": {"kind": "ElementReference", "name": "panel-" + str(panel["id"])},
        },
    }


V2_RESOURCE = {
    "apiVersion": "dashboard.grafana.app/v2",
    "spec": {
        "title": "V2 Matrix Reproduction",
        "timeSettings": {"from": "now-1h", "to": "now", "autoRefresh": False},
        "elements": {"panel-" + str(panel["id"]): v2_panel(panel) for panel in DASHBOARD["panels"]},
        "layout": {"kind": "GridLayout", "spec": {"items": [v2_layout_item(panel) for panel in DASHBOARD["panels"]]}},
        "variables": [{
            "kind": "DatasourceVariable",
            "spec": {
                "name": "datasource", "pluginId": "prometheus",
                "current": {"text": ["prom1"], "value": ["prom1"]},
            },
        }],
    },
}


V2_AUTO_RESOURCE = copy.deepcopy(V2_RESOURCE)
V2_AUTO_RESOURCE["spec"]["layout"] = {
    "kind": "AutoGridLayout",
    "spec": {
        "maxColumnCount": 3, "rowHeightMode": "short",
        "items": [
            {"kind": "AutoGridLayoutItem", "spec": {
                "element": item["spec"]["element"],
            }}
            for item in V2_RESOURCE["spec"]["layout"]["spec"]["items"]
        ],
    },
}

V2_LIBRARY_RESOURCE = copy.deepcopy(V2_RESOURCE)
V2_LIBRARY_RESOURCE["spec"]["elements"]["panel-13"] = {
    "kind": "LibraryPanel",
    "spec": {
        "id": 13, "title": "GPU-hours unavailable",
        "libraryPanel": {"uid": "matrix", "name": "Shared matrix"},
    },
}

V2_TABS_RESOURCE = copy.deepcopy(V2_RESOURCE)
V2_TABS_RESOURCE["spec"]["layout"] = {
    "kind": "TabsLayout",
    "spec": {"tabs": [
        {
            "kind": "TabsLayoutTab", "spec": {
                "title": "Overview",
                "layout": {"kind": "GridLayout", "spec": {
                    "items": V2_RESOURCE["spec"]["layout"]["spec"]["items"][:2],
                }},
            },
        },
        {
            "kind": "TabsLayoutTab", "spec": {
                "title": "Details",
                "layout": {"kind": "GridLayout", "spec": {
                    "items": V2_RESOURCE["spec"]["layout"]["spec"]["items"][2:],
                }},
            },
        },
    ]},
}


V2_REPEAT_TABS_RESOURCE = copy.deepcopy(V2_RESOURCE)
V2_REPEAT_TABS_RESOURCE["spec"]["title"] = "V2 Repeated Tabs"
V2_REPEAT_TABS_RESOURCE["spec"]["layout"] = {
    "kind": "TabsLayout",
    "spec": {"tabs": [
        {"kind": "TabsLayoutTab", "spec": {
            "title": "$cluster",
            "repeat": {"mode": "variable", "value": "cluster"},
            "layout": {"kind": "GridLayout", "spec": {
                "items": V2_RESOURCE["spec"]["layout"]["spec"]["items"][:1],
            }},
        }},
        {"kind": "TabsLayoutTab", "spec": {
            "title": "Static",
            "layout": {"kind": "GridLayout", "spec": {
                "items": V2_RESOURCE["spec"]["layout"]["spec"]["items"][1:],
            }},
        }},
    ]},
}
V2_REPEAT_TABS_RESOURCE["spec"]["variables"].append({
    "kind": "CustomVariable",
    "spec": {
        "name": "cluster", "query": "alpha,beta",
        "current": {"text": ["All"], "value": ["$__all"]},
        "options": [
            {"text": ["alpha"], "value": ["alpha"]},
            {"text": ["beta"], "value": ["beta"]},
        ],
        "multi": True, "includeAll": True,
    },
})


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/api/dashboards/uid/repro":
            self.send_json({"dashboard": DASHBOARD})
        elif self.path == "/apis/dashboard.grafana.app/v2/namespaces/default/dashboards/v2demo":
            self.send_json(V2_RESOURCE)
        elif self.path == "/apis/dashboard.grafana.app/v2/namespaces/default/dashboards/v2auto":
            self.send_json(V2_AUTO_RESOURCE)
        elif self.path == "/apis/dashboard.grafana.app/v2/namespaces/default/dashboards/v2tabs":
            self.send_json(V2_TABS_RESOURCE)
        elif self.path == "/apis/dashboard.grafana.app/v2/namespaces/default/dashboards/v2library":
            self.send_json(V2_LIBRARY_RESOURCE)
        elif self.path == "/apis/dashboard.grafana.app/v2/namespaces/default/dashboards/v2repeattabs":
            self.send_json(V2_REPEAT_TABS_RESOURCE)
        elif self.path == "/api/library-elements/matrix":
            self.send_json({"result": {"model": PANEL}})
        elif self.path == "/api/datasources":
            self.send_json([{"uid": "prom1", "name": "Prometheus", "type": "prometheus"}])
        elif self.path.startswith("/api/prometheus/grafana/api/v1/rules"):
            self.send_json({"data": {"groups": [{
                "name": "storage", "file": "Operations", "rules": [{
                    "name": "Disk full", "state": "firing", "type": "alerting",
                    "annotations": {"__dashboardUid__": "repro"},
                    "alerts": [{
                        "state": "firing", "activeAt": "2026-11-15T10:00:00Z",
                        "labels": {"severity": "critical", "instance": "node-a"},
                    }],
                }, {
                    "name": "CPU normal", "state": "inactive", "type": "alerting",
                    "annotations": {"__dashboardUid__": "repro"},
                    "alerts": [{"state": "Normal", "activeAt": "2026-11-15T10:00:00Z", "labels": {}}],
                }],
            }]}})
        elif self.path.startswith("/api/prometheus/prom1/api/v1/rules"):
            self.send_json({"data": {"groups": [{
                "name": "availability", "file": "Operations", "rules": [{
                    "name": "Prometheus down", "state": "firing", "type": "alerting",
                    "annotations": {"__dashboardUid__": "repro"},
                    "alerts": [{
                        "state": "firing", "activeAt": "2026-11-15T10:01:00Z",
                        "labels": {"severity": "warning", "instance": "prom-a"},
                    }],
                }],
            }]}})
        elif self.path.startswith("/api/search"):
            self.send_json([{"uid": "repro", "title": DASHBOARD["title"], "type": "dash-db"}])
        else:
            self.send_error(404)

    def do_POST(self):
        if self.path != "/api/ds/query":
            self.send_error(404)
            return
        payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        requested = {query["refId"] for query in payload["queries"]}
        results = {
            "A": {"frames": [frame("A", 1, "alpha"), frame("A", 5, "beta")]},
            "B": {"frames": [frame("B", 2, "alpha"), frame("B", 6, "beta")]},
            "C": {"frames": [frame("C", 3, "alpha"), frame("C", 7, "beta")]},
            "D": {"frames": [frame("D", 4, "alpha"), frame("D", 8, "beta")]},
            "E": {"frames": [frame("E", 0.995, "slo")]},
            "F": {"frames": [frame("F", 1, "slo")]},
            "G": {"frames": [frame("G", 111, "gpu-0")]},
            "H": {"frames": [frame("H", 333, "gpu-1")]},
            "I": {"frames": [geo_frame()]},
            "J": {"frames": [vendor_frame("Supermicro", 717), vendor_frame("Dell Inc.", 366), vendor_frame("Lenovo", 149)]},
            "K": {"frames": [vendor_model_frame()]},
            "L": {"frames": [geohash_frame()]},
        }
        self.send_json({"results": {ref: results[ref] for ref in requested}})

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
    ThreadingHTTPServer(("127.0.0.1", 18081), Handler).serve_forever()
