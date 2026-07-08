# personal-space

`personal-space` is the built-in CampusOS plugin metadata directory for personal homepages, default style packages, safe custom HTML style snippets, and local personal-space file storage.

The current implementation still lives in `internal/space/`. This plugin directory provides the default plugin manifest, keeps bundled style packages with the feature they belong to, and owns the default storage configuration used by the server.

## Files

| File | Purpose |
| --- | --- |
| `plugin.yaml` | Built-in plugin manifest and config schema shown by the plugin manager. |
| `styles/*.space-style.json` | Default personal homepage style packages. |

Personal homepage HTML snippets are stored in the applied `user_spaces.style_manifest` as `custom_html_enabled` and `custom_html`. They are validated by the backend before apply and rendered only after passing the restricted HTML safety rules.

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

Validate personal-space file storage:

```bash
GOCACHE=/tmp/campusos-go-cache go test ./internal/space -run 'Test(LocalFileStore|UploadAvatar)' -count=1
```
