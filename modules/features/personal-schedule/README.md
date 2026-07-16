# Personal Schedule built-in feature

Personal Schedule is a compiled Built-in Feature implemented in
`internal/modules/features/schedule`. Its descriptor is
`modules/features/personal-schedule/module.yaml`; it is never installed or
uninstalled as an External Plugin.

The feature uses restart activation. Disabling it hides the `/schedule` UI and
rejects schedule APIs after restart, without deleting any semester JSON or
imported file.

Schedule files are stored through User Storage Core:

```text
data/personal-space/<user_id>/file/schedule/
├── index.json
└── terms/
    ├── 2026-spring.json
    └── 2026-fall.json
```

One term JSON represents exactly one year/semester. `index.json` records the
selected term. Users can set the first-week date, browse week and calendar
views, edit JSON, edit courses manually, and import `.xls`, `.csv`, or `.json`
into the selected term. The **Current week** action returns to today's date.

The external `examples/plugins/schedule-helper` project is only a tutorial for
Host API, Manifest v2, grants, and managed records. It does not import this
internal package or read private schedule files.

Validate with:

```bash
GOCACHE=/tmp/campusos-go-cache go test ./internal/modules/features/schedule -count=1
```
