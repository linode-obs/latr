local mixin = import 'mixin.libsonnet';

// Emits one file per dashboard into the directory passed via -m.
{
  [name]: mixin.grafanaDashboards[name]
  for name in std.objectFields(mixin.grafanaDashboards)
}
