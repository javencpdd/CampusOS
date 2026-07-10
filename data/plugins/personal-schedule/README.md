# personal-schedule

`personal-schedule` is the built-in CampusOS plugin for each user's personal timetable.

The implementation lives in `internal/schedule`. This plugin directory provides the built-in plugin manifest, default configuration, and admin-visible metadata.

## Storage

Schedule data is stored with the user's personal-space files:

```text
data/personal-space/<user_id>/file/schedule/schedule.json
```

Because the file is under the same personal-space user directory, it is counted by personal-space local quota and can later be moved to a cloud-drive backed provider. The storage root is owned by the `personal-space` plugin; `personal-schedule` does not maintain a separate data root.

The JSON records the term with `term_year` and `semester` (`spring` or `fall`), as well as `first_week_start`. Existing schedule files without those two term fields remain readable: the service supplies the current year and the season inferred from the current date until the user saves the schedule.

## Current Capabilities

| Capability | Status |
| --- | --- |
| Term year and semester | Supported; select a year and spring or fall semester |
| First week start date | Supported |
| Week timetable view | Supported; switch to previous, current, or later weeks |
| Calendar view | Supported; browse past or future months and display courses by first-week date, weekday, and course weeks |
| Manual course editing | Supported |
| Raw JSON editing | Supported |
| `.xls` spreadsheet import | Supported for the current CampusOS course-table template |
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
