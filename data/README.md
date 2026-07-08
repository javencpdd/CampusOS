# CampusOS Data Directory

`data/` is the default local data root for runtime and distributable CampusOS assets.

| Directory | Purpose |
| --- | --- |
| `plugins/` | Installed and built-in plugin implementation directories. The server scans this path by default through `PLUGINS_DIR=data/plugins`; built-ins currently include `personal-space` and `homepage-customizer`. Keep plugin manifests, runtime entry files, and bundled plugin code here. |
| `plugin_data/` | Plugin runtime data, including SQLite-backed Host API storage and source-folder page style packs. This path is configured by `PLUGIN_DATA_DIR=data/plugin_data`. |
| `images/` | Local image, avatar, and upload assets. `personal-space` stores default personal files under `images/personal-space/`. |
| `dist/` | Release or deployment artifacts reserved for local packaging output. |
| `config/` | Local runtime configuration files. Do not commit secrets. |
| `skills/` | Local runtime or imported skill assets. Project workflow skills remain in the repository-level `skills/` directory. |

Most runtime data directories keep only placeholder files in git. Source-folder page style packs under `data/plugin_data/<plugin>/style-packs/` are versioned examples because they are editable plugin data rather than plugin implementation code.
