#!/usr/bin/env bash
# Checks the deployment assets: Helm chart (lint, render, Kubernetes schemas),
# alerting rules (promtool, sync with the Helm template) and compose files.
# Requires helm, kubeconform, promtool and docker on the PATH; run it from the
# repository root.
set -euo pipefail

update_rules=false
[ "${1:-}" = "--update-rules" ] && update_rules=true

chart=deployments/kubernetes/helm/ntp-exporter
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Schemas of the Prometheus Operator resources (ServiceMonitor, Probe...)
crds='https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json'

echo "== Helm chart"
for values in "$chart"/values*.yaml; do
  echo "-- $(basename "$values")"
  # values.yaml leaves the mode to the user
  mode=()
  [ "$(basename "$values")" = values.yaml ] && mode=(--set mode=agent)
  helm lint --strict "$chart" -f "$values" "${mode[@]}" >/dev/null
  helm template ci "$chart" -f "$values" "${mode[@]}" \
    --set serviceMonitor.enabled=true --set prometheusRule.enabled=true \
    >"$work/rendered.yaml"
  kubeconform -strict -summary -kubernetes-version 1.30.0 \
    -schema-location default -schema-location "$crds" "$work/rendered.yaml"
done

echo "== Alerting rules"
# Also check that a chart without mode refuses to render
if helm template ci "$chart" >/dev/null 2>&1; then
  echo "the chart must require a mode" >&2
  exit 1
fi

# The plain rules files are the spec of the PrometheusRule rendered for each
# mode with the default values
mkdir -p deployments/prometheus
for mode in probe agent; do
  rules="deployments/prometheus/ntp-alerts-$mode.yml"
  helm template ci "$chart" --set mode="$mode" --set probeEndpoint.enabled=true --set prometheusRule.enabled=true \
    -s templates/prometheusrule.yaml |
    sed -n '/^spec:/,$p' | tail -n +2 | sed 's/^  //' | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}' >"$work/from-helm.yml"
  if $update_rules; then
    {
      echo "# NTP exporter alerting rules for plain Prometheus (rule_files), $mode mode."
      echo "# Generated from the Helm PrometheusRule template (deployments/kubernetes/helm/ntp-exporter/templates/prometheusrule.yaml):"
      echo "# .github/scripts/check-deployments.sh --update-rules"
      cat "$work/from-helm.yml"
    } >"$rules"
  fi
  sed -n '/^groups:/,$p' "$rules" >"$work/from-file.yml"
  if ! diff -u "$work/from-file.yml" "$work/from-helm.yml"; then
    echo "$rules differs from the PrometheusRule template, regenerate it:" >&2
    echo "  .github/scripts/check-deployments.sh --update-rules" >&2
    exit 1
  fi
  promtool check rules --lint=all "$rules"
done

echo "== Compose files"
for compose in deployments/docker/docker-compose-*.yml; do
  docker compose -f "$compose" config --quiet
  echo "-- $(basename "$compose") ok"
done
