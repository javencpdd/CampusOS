# personal-space

`personal-space` is the built-in CampusOS plugin metadata directory for personal homepages, default JSON style packages, safe custom HTML/CSS style snippets, and local personal-space file storage.

The current implementation still lives in `internal/space/`. This plugin directory provides the default plugin manifest, keeps plugin-bundled JSON style packages with the feature they belong to, and owns the default storage configuration used by the server. Editable source-folder page style packs are plugin data and live under `data/plugin_data/personal-space/style-packs/`.

## Files

| File | Purpose |
| --- | --- |
| `plugin.yaml` | Built-in plugin manifest and config schema shown by the plugin manager. |
| `styles/*.space-style.json` | Default personal homepage style packages. |

Personal homepage HTML/CSS snippets are stored in the applied `user_spaces.style_manifest` as `custom_html_enabled`, `custom_html`, and `custom_css`. They are validated by the backend before apply and rendered only after passing the restricted HTML and CSS safety rules.

## Page Style Packs

`page-style-pack.v1` is the folder/zip format for more open page styling. A pack can be edited as a directory under `data/plugin_data/personal-space/style-packs/<name>/`, or zipped and uploaded from the user frontend.

Source package structure:

```text
style.yaml
README.md
preview.png
templates/page.html
templates/card.html
assets/cover.webp
assets/avatar-frame.png
styles/theme.css
config.schema.json
```

Required manifest keys:

```yaml
schema_version: page-style-pack.v1
target: personal-space
name: clean-blog
version: 0.1.0
entry: templates/page.html
templates:
  - name: page
    path: templates/page.html
  - name: card
    path: templates/card.html
styles:
  - styles/theme.css
preview_image: preview.png
config_schema: config.schema.json
assets:
  - name: cover
    path: assets/cover.webp
    type: image/webp
  - name: avatar-frame
    path: assets/avatar-frame.png
    type: image/png
```

Safety screening is handled by `internal/stylepack`: safe relative paths only, limited file count and size, allowed extensions, restricted HTML for every template, restricted CSS for every stylesheet, image asset path checks, and `config.schema.json` JSON parsing. A pack must pass screening before it can be applied.

User-facing endpoints:

```text
POST /api/v1/spaces/me/styles/packs/validate
GET  /api/v1/spaces/me/styles/packs/example
GET  /api/v1/spaces/me/styles/packs/example.zip
POST /api/v1/spaces/me/styles/packs/apply
POST /api/v1/spaces/me/styles/packs/apply-source
```

## Default Config

| Key | Default | Purpose |
| --- | --- | --- |
| `styles_dir` | `styles` | Style package directory relative to this plugin. |
| `file_root` | `data/personal-space` | Local root for user personal-space files. |
| `file_url_prefix` | `/api/v1/spaces/files` | Public API prefix for stored files. |
| `default_quota_mb` | `10` | Initial local storage quota per user. |
| `avatar_keep_limit` | `3` | Keep the latest 3 avatar source files per user. |
| `max_avatar_mb` | `2` | Maximum size of one avatar upload. |
| `future_storage_provider` | `local` | Reserved selector for future personal cloud drive integration. |

Each user has one direct storage root. The current layout is:

```text
data/personal-space/<user_id>/
  file/
    schedule/schedule.json
  img/
    avatars/
  excel/
  word/
  pdf/
```

Avatar uploads are stored under `img/avatars/` and the latest three source files are retained. Ordinary uploads are assigned by extension: image files to `img/`, `.xls`/`.xlsx`/`.csv` to `excel/`, Word-compatible files to `word/`, PDF files to `pdf/`, and other files to `file/`. Data saved by a feature without an uploaded extension can use a named subdirectory under `file/`; the personal schedule uses `file/schedule/`.

On startup, the former default layout `data/images/personal-space/users/<user_id>/` is migrated into this structure. Existing public avatar URLs remain unchanged.

Public avatar URLs use:

```text
/api/v1/spaces/files/<user_id>/avatars/<filename>
```

## Validate

Validate style packages:

```bash
GOCACHE=/tmp/campusos-go-cache go test ./internal/space -run TestExampleStylePackagesAreValid -count=1
```

Validate page style packs:

```bash
GOCACHE=/tmp/campusos-go-cache go test ./internal/stylepack ./internal/space -run 'Test(LoadZip|ZipBundle|ApplyStylePack)' -count=1
```

Validate personal-space file storage:

```bash
GOCACHE=/tmp/campusos-go-cache go test ./internal/space -run 'Test(LocalFileStore|UploadAvatar)' -count=1
```
