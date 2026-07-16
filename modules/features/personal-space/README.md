# Personal Space built-in feature

Personal Space is compiled into CampusOS and implemented in
`internal/modules/features/personalspace`. Its `module.yaml` owns feature
metadata, defaults, dependencies, configuration schema, and trusted Web UI
contributions. It is not installed or removed by the Plugin Manager.

The feature can be disabled with restart activation. Disabling it hides its UI
and rejects its feature APIs after restart, while preserving profiles, styles,
user files, and database records.

## Ownership

```text
modules/features/personal-space/                  module descriptor and docs
internal/modules/features/personalspace/          compiled feature code
data/module_data/personal-space/styles/           built-in JSON style data
data/resources/space-style-packs/                 validated Resource Packages
data/personal-space/<user_id>/                    files owned by User Storage Core
```

User Storage Core, not Personal Space, owns safe paths, quotas, avatar files,
rich-text images, schedule files, and future storage providers. Personal Space
uses those ports for its profile and file UI.

Default local quota is 10 MB, a single avatar upload is limited to 2 MB, and
the latest three avatar source files are retained. These defaults are changed
through `GET/PUT /api/v1/features/personal-space`, not a plugin manifest.

## Style packages

Folder and zip packages use `page-style-pack.v1`; checked-in packages also
carry `resource.json` with a type, compatibility range, source, entry, and
checksum. They must pass path, size, extension, HTML, CSS, asset, schema, and
effect validation before application.

Personal-space selectors are scoped below
`.public-space[data-campusos-space]`. Optional effects run only through the
restricted `sandbox-worker.v1`; they receive no DOM, same-origin credential,
storage, token, or arbitrary network access.

Useful checks:

```bash
GOCACHE=/tmp/campusos-go-cache go test ./internal/modules/features/personalspace ./internal/modules/features/appearance/stylepack -count=1
go run ./cmd/campusosctl resource inspect data/resources/space-style-packs/clean-blog
```
