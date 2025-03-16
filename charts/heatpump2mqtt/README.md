# Hass2CH Helm Chart

This Helm chart deploys the Home Assistant to ClickHouse data ingestion pipeline, which captures Home Assistant events and stores them in a ClickHouse database for analytics and historical data processing.

## Features

- Home Assistant event ingestion
- ClickHouse data storage
- Prometheus metrics
- Grafana dashboards
- Automatic retries for ClickHouse operations

## Installing the Chart

```bash
helm install hass2ch ./charts/hass2ch \
  --set homeAssistant.token=<your-hass-token> \
  --set clickhouse.url=http://clickhouse:8123
```

## Handling Sensitive Data

The chart provides two methods for handling sensitive data like credentials:

### 1. Create Secret from Values

The default mode is to create a Secret with values provided in the chart:

```yaml
# values.yaml
homeAssistant:
  token: "your-hass-token"  # Will be stored in Secret

clickhouse:
  password: "your-clickhouse-password"  # Will be stored in Secret

secret:
  mode: "create"  # Default, creates a Secret
```

### 2. Use Existing Secret

You can also reference an existing Secret:

```yaml
# values.yaml
secret:
  mode: "existing"
  existingSecretName: "my-existing-secret"
  hassTokenKey: "hass-token"  # Key in the existing secret for HA token
  clickhousePasswordKey: "clickhouse-password"  # Key in the existing secret for CH password
```

The existing Secret should be created before deploying the chart:

```bash
kubectl create secret generic my-existing-secret \
  --from-literal=hass-token=<your-hass-token> \
  --from-literal=clickhouse-password=<your-clickhouse-password>
```

## Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of replicas | `1` |
| `image.repository` | Image repository | `ghcr.io/jkaflik/hass2ch` |
| `image.tag` | Image tag | `""` (uses chart appVersion) |
| `homeAssistant.host` | Home Assistant host | `homeassistant.local` |
| `homeAssistant.secure` | Use secure WebSocket connection | `false` |
| `homeAssistant.token` | Home Assistant token | `""` |
| `clickhouse.url` | ClickHouse HTTP URL | `http://clickhouse:8123` |
| `clickhouse.database` | ClickHouse database | `hass` |
| `clickhouse.username` | ClickHouse username | `default` |
| `clickhouse.password` | ClickHouse password | `""` |
| `secret.mode` | Secret handling mode (`create` or `existing`) | `create` |
| `secret.existingSecretName` | Name of existing Secret to use | `""` |
| `metrics.enabled` | Enable Prometheus metrics | `true` |
| `config.clickhouse.maxRetries` | Maximum retries for ClickHouse operations | `5` |

See `values.yaml` for full list of configurable parameters.

## Observability

This chart provides comprehensive observability features:

### Prometheus Metrics

Enable the Prometheus ServiceMonitor:

```yaml
metrics:
  enabled: true
  serviceMonitor:
    enabled: true
```

### Grafana Dashboards

The chart includes pre-configured Grafana dashboards:

```yaml
dashboards:
  enabled: true
```

### AlertManager Rules

Enable AlertManager rules:

```yaml
metrics:
  prometheusRule:
    enabled: true
```