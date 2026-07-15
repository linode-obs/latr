{
  _config+:: {
    // Prometheus label selector for latr metrics, inserted into {} of queries.
    // Examples:
    //   'job="latr"'
    //   'namespace="security",job="latr"'
    // Leave empty to match all latr_* series (default).
    latrSelector: '',

    // Grafana dashboard metadata
    dashboardNamePrefix: '',
    dashboardTags: ['latr-mixin', 'latr', 'linode', 'token-rotation', 'vault'],
    grafanaDashboardUid: 'latr-token-rotator',
    dashboardRefresh: '1m',
    dashboardPeriod: 'now-1h',

    // Optional multi-cluster support (appended to by/group labels when true).
    showMultiCluster: false,
    clusterLabel: 'cluster',

    // Alert thresholds
    // Fire when any managed token has less than this many seconds until
    // the configured rotation threshold (default: 7 days).
    tokenValidityLowSeconds: 7 * 24 * 3600,
    // Window used for increase() / rate() based alerts.
    alertWindow: '15m',
    // Pending duration before alerts fire.
    alertFor: '5m',
  },
}
