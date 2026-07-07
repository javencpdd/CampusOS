# personal-space

`personal-space` is the built-in CampusOS plugin metadata directory for personal homepages and default style packages.

The current personal homepage implementation still lives in `internal/space/`. This plugin directory provides the default plugin manifest and keeps bundled style packages with the feature they belong to.

## Files

| File | Purpose |
| --- | --- |
| `plugin.yaml` | Built-in plugin manifest shown by the plugin manager. |
| `styles/*.space-style.json` | Default personal homepage style packages. |

## Validate Styles

```bash
GOCACHE=/tmp/campusos-go-cache go test ./internal/space -run TestExampleStylePackagesAreValid -count=1
```
