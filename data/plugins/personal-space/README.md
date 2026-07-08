# personal-space

`personal-space` is the built-in CampusOS plugin metadata directory for personal homepages, default style packages, page style-pack folders, safe custom HTML/CSS style snippets, and local personal-space file storage.

The current implementation still lives in `internal/space/`. This plugin directory provides the default plugin manifest, keeps bundled style packages with the feature they belong to, and owns the default storage configuration used by the server.

## Files

| File | Purpose |
| --- | --- |
| `plugin.yaml` | Built-in plugin manifest and config schema shown by the plugin manager. |
| `styles/*.space-style.json` | Default personal homepage style packages. |
| `style-packs/<name>/style.yaml` | Source-folder page style packs using `page-style-pack.v1`. |
| `style-packs/<name>/templates/page.html` | Restricted HTML entry for a folder style pack. |
| `style-packs/<name>/styles/theme.css` | Restricted CSS for a folder style pack. |

Personal homepage HTML/CSS snippets are stored in the applied `user_spaces.style_manifest` as `custom_html_enabled`, `custom_html`, and `custom_css`. They are validated by the backend before apply and rendered only after passing the restricted HTML and CSS safety rules.

## Page Style Packs

`page-style-pack.v1` is the folder/zip format for more open page styling. A pack can be edited as a directory under `style-packs/`, or zipped and uploaded from the user frontend.

Minimal structure:

```text
style.yaml
templates/page.html
styles/theme.css
README.md
```

Required manifest keys:

```yaml
schema_version: page-style-pack.v1
target: personal-space
name: clean-folder
version: 0.1.0
entry: templates/page.html
styles:
  - styles/theme.css
```

Safety screening is handled by `internal/stylepack`: safe relative paths only, limited file count and size, allowed extensions, restricted HTML, and restricted CSS. A pack must pass screening before it can be applied.

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
| `file_root` | `data/images/personal-space` | Local root for user personal-space files. |
| `file_url_prefix` | `/api/v1/spaces/files` | Public API prefix for stored files. |
| `default_quota_mb` | `10` | Initial local storage quota per user. |
| `avatar_keep_limit` | `3` | Keep the latest 3 avatar source files per user. |
| `max_avatar_mb` | `2` | Maximum size of one avatar upload. |
| `future_storage_provider` | `local` | Reserved selector for future personal cloud drive integration. |

Avatar uploads are stored under:

```text
data/images/personal-space/users/<user_id>/avatars/
```

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
