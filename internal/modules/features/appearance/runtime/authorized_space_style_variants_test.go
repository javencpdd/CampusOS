package appearance

func appearanceVariantFiles() map[string]string {
	return map[string]string{"style.yaml": `schema_version: page-style-pack.v1
target: personal-space
name: variant-space
version: 1.0.0
delivery_contract: campusos.appearance-delivery/v1
viewport_support:
  desktop: true
  mobile: true
  mobile_breakpoint: 720px
entry: templates/page.html
styles:
  - styles/theme.css
preview_images:
  desktop: preview-desktop.png
  mobile: preview-mobile.png
`, "templates/page.html": `<section class="cstyle-page"><h2>Current variant</h2></section>`, "styles/theme.css": `.public-space[data-campusos-space] .cstyle-page { padding: 16px; } @media (max-width: 720px) { .public-space[data-campusos-space] .cstyle-page { padding: 12px; } }`, "preview-desktop.png": "desktop preview", "preview-mobile.png": "mobile preview"}
}
