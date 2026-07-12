# web-theme

`web-theme` is the built-in system-level provider for themes that apply to the complete CampusOS user frontend. It never applies to the Admin frontend.

Theme source folders live at:

```text
data/plugin_data/web-theme/style-packs/<theme>/
```

Only administrators or deployed plugins should place packages in that directory. Users can select from valid packages, but cannot install arbitrary system themes through the user frontend.

`data/plugins/web-theme/` contains only plugin implementation metadata. Theme manifests, templates, CSS, images, previews, effects, and configuration schemas are data and must remain under `data/plugin_data/web-theme/`.

System theme enable/disable follows the system-plugin restart lifecycle. Theme selection and permission consent are local, per user, and take effect without restarting the API.

The package target must be `web`. Every CSS selector must start with `.app-container[data-campusos-web]`. Optional effects run as `sandbox-worker.v1`; they receive no JWT, DOM access, storage access or arbitrary network access. Read-only CampusStyleSDK capabilities are declared in `style.yaml` and are checked by the host.

Every built-in package declares desktop/mobile support and includes a responsive width media rule. `config.schema.json` may bind safe user controls to declared tokens and layout fields, allowing text color, background asset, overlay, page padding, and related values to change without editing package code. The loader checks declared color contrast and the Web settings page blocks unreadable local color combinations.

Public catalog API:

```text
GET /api/v1/web-themes
GET /api/v1/web-themes/:name
GET /api/v1/web-themes/:name/assets/*path
```
