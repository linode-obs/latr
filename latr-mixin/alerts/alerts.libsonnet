{
  local cfg = self._config,

  // Build a Prometheus selector string for use inside {}.
  // extra is an optional comma-free label matcher list, e.g. 'status="failure"'.
  local selector(extra='') =
    local parts =
      (if cfg.latrSelector != '' then [cfg.latrSelector] else []) +
      (if extra != '' then [extra] else []);
    if std.length(parts) == 0 then '' else '{%s}' % std.join(',', parts),

  local sel = selector(),
  local selFailure = selector('status="failure"'),

  prometheusAlerts+:: {
    groups+: [
      {
        name: 'latr',
        rules: [
          {
            alert: 'LatrTokenRotationFailed',
            expr: |||
              sum by (label) (
                increase(latr_rotations_total%(selFailure)s[%(window)s])
              ) > 0
            ||| % {
              selFailure: selFailure,
              window: cfg.alertWindow,
            },
            'for': cfg.alertFor,
            labels: {
              severity: 'warning',
            },
            annotations: {
              summary: 'latr failed to rotate a Linode API token.',
              description: |||
                Token label {{ $labels.label }} has {{ $value | humanize }} failed rotation attempt(s) in the last %(window)s.
                Check latr logs for Linode API or Vault errors. After a successful retry, complete CSI follow-up if required.
              ||| % { window: cfg.alertWindow },
            },
          },
          {
            alert: 'LatrVaultStorageErrors',
            expr: |||
              sum by (path) (
                increase(latr_vault_storage_errors_total%(sel)s[%(window)s])
              ) > 0
            ||| % {
              sel: sel,
              window: cfg.alertWindow,
            },
            'for': cfg.alertFor,
            labels: {
              severity: 'critical',
            },
            annotations: {
              summary: 'latr failed to write a rotated token to Vault.',
              description: |||
                Vault path {{ $labels.path }} had {{ $value | humanize }} storage error(s) in the last %(window)s.
                Linode and Vault may be out of sync — do not revoke tokens until storage succeeds and consumers are updated.
              ||| % { window: cfg.alertWindow },
            },
          },
          {
            alert: 'LatrTokenValidityLow',
            expr: |||
              min by (label) (
                latr_token_validity_remaining_seconds%(sel)s
              ) < %(threshold)s
            ||| % {
              sel: sel,
              threshold: cfg.tokenValidityLowSeconds,
            },
            'for': cfg.alertFor,
            labels: {
              severity: 'warning',
            },
            annotations: {
              summary: 'A managed Linode API token is close to its rotation threshold.',
              description: |||
                Token label {{ $labels.label }} has only {{ $value | humanizeDuration }} remaining until latr considers rotation needed (threshold %(threshold)s).
                Confirm latr is running and able to reach Linode and Vault.
              ||| % { threshold: cfg.tokenValidityLowSeconds },
            },
          },
          {
            alert: 'LatrDown',
            // No samples for the configured-token gauge → not scraping / not exporting.
            expr: 'absent(latr_tokens_total%(sel)s)' % { sel: sel },
            'for': '15m',
            labels: {
              severity: 'warning',
            },
            annotations: {
              summary: 'latr metrics are missing.',
              description: |||
                latr_tokens_total has been absent for 15m (selector: %(selector)s).
                The daemon may be down, misconfigured, or metrics may not be reaching Prometheus.
              ||| % {
                selector: if cfg.latrSelector == '' then '(all)' else cfg.latrSelector,
              },
            },
          },
        ],
      },
    ],
  },
}
