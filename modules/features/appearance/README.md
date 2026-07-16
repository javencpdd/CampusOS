# Appearance module

`feature.appearance` is the compiled Built-in Feature that owns the public
facade for homepage layout, complete Web themes, personal-space style packs,
and user appearance preferences.

The code is under `internal/modules/features/appearance/`. Its module contract
and default configuration are in `module.yaml`. Theme content is not module
code and is stored as Resource Packages:

```text
data/resources/
  themes/
  homepage-packs/
  space-style-packs/
```

The historical names `homepage-customizer` and `web-theme` remain accepted by
the Feature API as configuration aliases. They are not installed plugins and
do not appear in the external plugin catalog.

All HTML, CSS, assets, responsive declarations, effects, checksums, and package
metadata must pass the Resource Package and style-pack validators before use.
