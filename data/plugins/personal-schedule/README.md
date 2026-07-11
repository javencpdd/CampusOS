# personal-schedule

`personal-schedule` is the built-in CampusOS plugin for each user's personal timetable.

The implementation lives in `internal/schedule`. This plugin directory provides the built-in plugin manifest, default configuration, and admin-visible metadata.

## Lifecycle

This is a `scope: system` plugin. Enabling or disabling it in Admin records the target state, and the change applies after the CampusOS API service restarts.

## Storage

Schedule data is stored with the user's personal-space files:

```text
data/personal-space/<user_id>/file/schedule/
├── index.json
└── terms/
    ├── 2026-spring.json
    └── 2026-fall.json
```

Each `terms/<year>-<semester>.json` file represents exactly one semester timetable. `index.json` only records which saved timetable the user last selected. Importing an Excel, CSV, or JSON file writes only to the currently selected semester file, so spring and fall data never overwrite each other.

Because these files are under the same personal-space user directory, they are counted by personal-space local quota and can later be moved to a cloud-drive backed provider. The storage root is owned by the `personal-space` plugin; `personal-schedule` does not maintain a separate data root.

Older `file/schedule/schedule.json` data is migrated automatically on first read into the matching `terms/<year>-<semester>.json` file. Existing files without term fields use the current year and season as a compatibility default.

## Current Capabilities

| Capability | Status |
| --- | --- |
| Independent semester JSON | Supported; one `terms/<year>-<semester>.json` file for each saved timetable |
| Term selection | Supported; choose a saved semester or enter a year and season, then use **Open/New timetable** to switch or create it |
| First week start date | Supported through the **Set first week** button for the current semester |
| Week timetable view | Supported; switch to previous, current, or later weeks; **Current week** always returns the calendar to today's date |
| Calendar view | Supported; browse past or future months and display courses by first-week date, weekday, and course weeks |
| Manual course editing | Supported |
| Raw JSON editing | Supported for the currently opened semester; it does not switch or create a timetable |
| `.xls` spreadsheet import | Supported for the current CampusOS course-table template; imports into the current semester JSON only |
| `.csv` / `.json` import | Supported |
| Cloud drive sync | Reserved for later |

## Import Columns

The `.xls` importer understands the sample shape under `docs/Todo/tmp/class.xls`:

```text
课程编号, 课程名称, 任课教师, 开始周, 结束周, 时间, 上课地点
```

`时间` accepts values such as:

```text
星期三 上午3-上午4
星期二 下午5-下午7
```

The importer converts these rows into `weekday`, `start_period`, `end_period`, and `weeks`.
