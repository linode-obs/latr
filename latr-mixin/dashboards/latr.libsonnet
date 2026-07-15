local raw = import 'latr.json';

// Apply the latr Prometheus selector to every PromQL expression in the
// dashboard. The raw JSON uses __LATR_SELECTOR__ as a placeholder.
local applySelector(expr, selector) =
  if selector == '' then
    // Bare metric: {__LATR_SELECTOR__} → remove braces entirely
    // With labels: {__LATR_SELECTOR__,foo="bar"} → {foo="bar"}
    std.strReplace(
      std.strReplace(expr, '{__LATR_SELECTOR__,', '{'),
      '{__LATR_SELECTOR__}',
      ''
    )
  else
    std.strReplace(expr, '__LATR_SELECTOR__', selector);

local mapDashboard(obj, selector) =
  if std.isObject(obj) then {
    [k]:
      if std.member(['expr', 'definition'], k) && std.isString(obj[k]) then
        applySelector(obj[k], selector)
      else if k == 'query' && std.isString(obj[k]) then
        applySelector(obj[k], selector)
      else
        mapDashboard(obj[k], selector)
    for k in std.objectFields(obj)
  }
  else if std.isArray(obj) then
    [mapDashboard(x, selector) for x in obj]
  else
    obj;

{
  local cfg = self._config,

  grafanaDashboards+:: {
    'latr.json':
      mapDashboard(raw, cfg.latrSelector) {
        uid: cfg.grafanaDashboardUid,
        title: cfg.dashboardNamePrefix + super.title,
        tags: cfg.dashboardTags,
        refresh: cfg.dashboardRefresh,
        time: {
          from: cfg.dashboardPeriod,
          to: 'now',
        },
      },
  },
}
