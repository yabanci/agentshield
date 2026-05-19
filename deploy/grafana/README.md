# AgentShield — Grafana Dashboard & Prometheus Alerts

Grafana 10+ dashboard and Prometheus alert rules for the AgentShield resilience proxy.

## Requirements

- Grafana **10.0 or newer** (state timeline panel is required; it was promoted to stable in Grafana 10)
- Prometheus scraping `/metrics` on the AgentShield process

---

## Importing the dashboard

1. Open Grafana → **Dashboards → New → Import**
2. Drag `agentshield-dashboard.json` into the upload area (or paste its contents)
3. In the "Prometheus" dropdown, select your Prometheus datasource
4. Click **Import**

One-liner to copy the JSON to your clipboard on macOS:

```bash
pbcopy < deploy/grafana/agentshield-dashboard.json
```

The dashboard uses `${DS_PROMETHEUS}` as a templated datasource variable, so it imports cleanly into any cluster regardless of datasource UID.

---

## Applying the alert rules

### Option A — Kubernetes (PrometheusOperator / kube-prometheus-stack)

The file `deploy/prometheus/alerts.yaml` is a `PrometheusRule` CRD. Apply it directly:

```bash
kubectl apply -f deploy/prometheus/alerts.yaml
```

The PrometheusOperator watches for `PrometheusRule` objects with the label
`prometheus: kube-prometheus` and hot-reloads them without restarting Prometheus.
Adjust the label selector in `metadata.labels` if your operator uses a different
`ruleSelector`.

### Option B — Standalone Prometheus (`rule_files:` in prometheus.yml)

1. Copy the rule block (the `groups:` key and everything under it) into a standalone file, e.g. `rules/agentshield.yml`
2. Reference it in `prometheus.yml`:

```yaml
rule_files:
  - "rules/agentshield.yml"
```

3. Reload Prometheus:

```bash
curl -X POST http://localhost:9090/-/reload
```

### Validating rules with promtool

```bash
promtool check rules deploy/prometheus/alerts.yaml
```

---

## Dashboard panels (16 total, 4 rows)

| Row | Panel | Type |
|---|---|---|
| Request Flow | Requests/sec by tier | Time series |
| Request Flow | Fallback rate % (5m) | Stat |
| Request Flow | Graceful denial rate % (5m) | Stat |
| Request Flow | Cache hits (5m) | Stat |
| Request Flow | Cache size | Stat |
| Quality | Quality score per model | Time series |
| Quality | Semantic CB state per model | State timeline |
| Quality | Transport CB state per model | State timeline |
| Latency | Request latency p50/p95 by tier | Time series |
| Latency | Request duration heatmap | Heatmap |
| Defenses | Load-shed events/sec | Time series |
| Defenses | Bulkhead-full events/sec by type | Time series |
| Defenses | Webhook drops (5m) | Stat |
| Defenses | Hedge fires/sec | Time series |

---

## Alert rules (7 total)

| Alert | Severity | Fires when |
|---|---|---|
| `AgentShieldSemanticBreakerFailing` | warning | Semantic CB > 1 for 2m |
| `AgentShieldTransportBreakerOpen` | warning | Transport CB == 2 for 2m |
| `AgentShieldFallbackRateHigh` | warning | Fallback share > 30% for 10m |
| `AgentShieldGracefulDenialBurst` | critical | Denials > 5/s for 1m |
| `AgentShieldWebhookDrops` | warning | Any webhook drop in 5m window |
| `AgentShieldP95Spike` | warning | Primary p95 > 5s for 5m |
| `AgentShieldLoadshedSustained` | critical | Load-shed > 1/s for 5m |

Runbook URLs follow the pattern:
`https://github.com/yabanci/agentshield/blob/main/docs/runbooks/<AlertName>.md`

---

## Screenshot

![dashboard](dashboard-screenshot.png)

_Screenshot taken during a chaos run: transport CB opens at ~0:20, semantic CB
recovers independently at ~0:45, demonstrating the two-stack independence._

---

## Notes

- The state timeline panels require Grafana 10+. On Grafana 9.x the panels will
  fall back to a basic graph; upgrade or replace with a time-series showing the
  raw gauge value.
- All panels use a 15-second auto-refresh; lower to 5s during live demos.
- The heatmap uses `format: heatmap` (legacy Grafana format) which works with
  the standard Prometheus histogram format emitted by the Go client.
