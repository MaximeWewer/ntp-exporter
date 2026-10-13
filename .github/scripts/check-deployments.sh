#!/usr/bin/env bash
# Checks the deployment assets: Helm chart (lint, render, Kubernetes schemas),
# alerting rules (promtool, sync with the Helm template) and compose files.
# Requires helm, kubeconform, promtool and docker on the PATH; run it from the
# repository root.
set -euo pipefail

update_rules=false
[ "${1:-}" = "--update-rules" ] && update_rules=true

chart=deployments/kubernetes/helm/ntp-exporter
rules=deployments/prometheus/ntp-alerts.yml
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

# The plain rules file is the spec of the PrometheusRule rendered with the
# default values
# Also check that a chart without mode refuses to render
if helm template ci "$chart" >/dev/null 2>&1; then
  echo "the chart must require a mode" >&2
  exit 1
fi
helm template ci "$chart" --set mode=probe --set probeEndpoint.enabled=true --set prometheusRule.enabled=true -s templates/prometheusrule.yaml |
  sed -n '/^spec:/,$p' | tail -n +2 | sed 's/^  //' | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}' >"$work/from-helm.yml"
if $update_rules; then
  { sed -n '1,/^groups:/p' "$rules" | sed '$d'; cat "$work/from-helm.yml"; } >"$work/rules.yml"
  mv "$work/rules.yml" "$rules"
fi
sed -n '/^groups:/,$p' "$rules" >"$work/from-file.yml"
if ! diff -u "$work/from-file.yml" "$work/from-helm.yml"; then
  echo "$rules differs from the PrometheusRule template, regenerate it:" >&2
  echo "  .github/scripts/check-deployments.sh --update-rules" >&2
  exit 1
fi

promtool check rules --lint=all "$rules"

echo "== Compose files"
for compose in deployments/docker/docker-compose-*.yml; do
  docker compose -f "$compose" config --quiet
  echo "-- $(basename "$compose") ok"
done
