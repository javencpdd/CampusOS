# homepage-customizer

`homepage-customizer` is a built-in CampusOS plugin for safe homepage customization on the user web app at `http://localhost:3000/`.

The implementation reads this plugin's config from the backend plugin manager and exposes a sanitized public API at:

```text
GET /api/v1/home/config
```

Admin users can edit the config from the admin Plugin Management page by opening this plugin's config dialog. The same dialog also supports importing a folder-designed page style pack as a zip file.

## Current Config

| Key | Purpose |
| --- | --- |
| `hero_title` | Homepage hero title. |
| `hero_subtitle` | Homepage hero subtitle. |
| `background_image` | Optional homepage background image URL. |
| `background_overlay` | Overlay color applied over the background image. |
| `show_category_tags` | Whether to show category quick filter tags. |
| `category_tag_limit` | Maximum category tags shown. |
| `custom_html_enabled` | Whether to render a validated custom HTML snippet. |
| `custom_html` | Restricted HTML snippet rendered after backend validation. |
| `custom_css` | Restricted CSS snippet rendered with `custom_html` after backend validation. |
| `active_style_pack` | Name of the last applied homepage page style pack. |
| `style_pack_version` | Version of the last applied homepage page style pack. |

## Page Style Packs

Homepage page style packs use the same `page-style-pack.v1` folder/zip standard as personal spaces, but must set:

```yaml
target: homepage
```

Source-folder examples live under plugin data:

```text
data/plugin_data/homepage-customizer/style-packs/
```

Minimal pack structure:

```text
style.yaml
templates/page.html
styles/theme.css
README.md
```

Admin endpoints:

```text
POST /api/v1/home/style-packs/validate
GET  /api/v1/home/style-packs/example
GET  /api/v1/home/style-packs/example.zip
POST /api/v1/home/style-packs/apply
POST /api/v1/home/style-packs/apply-source
```

Applying a zip or source-folder pack writes the screened HTML/CSS into the plugin config and records the active pack name/version.

## Safety Boundary

The backend rejects scripts, event handler attributes, unsafe URLs, dangerous CSS, unsafe paths, excessive nesting, oversized snippets, oversized packs, and unsupported file types before config is saved. The public config endpoint also hides invalid legacy HTML/CSS before the user frontend renders it.

Arbitrary JavaScript and unsandboxed HTML are not enabled.
