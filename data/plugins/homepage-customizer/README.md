# homepage-customizer

`homepage-customizer` is a built-in CampusOS plugin for safe homepage customization on the user web app at `http://localhost:3000/`.

The implementation reads this plugin's config from the backend plugin manager and exposes a sanitized public API at:

```text
GET /api/v1/home/config
```

Admin users can edit the config from the admin Plugin Management page by opening this plugin's config dialog.

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

## Safety Boundary

The backend rejects scripts, event handler attributes, unsafe URLs, dangerous CSS, excessive nesting, and oversized snippets before config is saved. The public config endpoint also hides invalid legacy HTML before the user frontend renders it.

Arbitrary JavaScript and unsandboxed HTML are not enabled.
