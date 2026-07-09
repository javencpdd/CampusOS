package schedule

import "time"

const PluginName = "personal-schedule"

type Course struct {
	ID          string            `json:"id"`
	Code        string            `json:"code,omitempty"`
	Name        string            `json:"name"`
	Teacher     string            `json:"teacher,omitempty"`
	Location    string            `json:"location,omitempty"`
	Weekday     int               `json:"weekday"`
	StartPeriod int               `json:"start_period"`
	EndPeriod   int               `json:"end_period"`
	Weeks       []int             `json:"weeks,omitempty"`
	Color       string            `json:"color,omitempty"`
	Note        string            `json:"note,omitempty"`
	Source      string            `json:"source,omitempty"`
	Extra       map[string]string `json:"extra,omitempty"`
}

type Settings struct {
	PeriodsPerDay int      `json:"periods_per_day"`
	ShowWeekend   bool     `json:"show_weekend"`
	PeriodLabels  []string `json:"period_labels,omitempty"`
}

type Schedule struct {
	UserID         string                 `json:"user_id"`
	FirstWeekStart string                 `json:"first_week_start"`
	Settings       Settings               `json:"settings"`
	Courses        []Course               `json:"courses"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

type UpsertRequest struct {
	FirstWeekStart string                 `json:"first_week_start"`
	Settings       Settings               `json:"settings"`
	Courses        []Course               `json:"courses"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

type WeekInfo struct {
	CurrentWeek int    `json:"current_week"`
	WeekStart   string `json:"week_start"`
	WeekEnd     string `json:"week_end"`
	Today       string `json:"today"`
}

type ScheduleResponse struct {
	Schedule *Schedule `json:"schedule"`
	Week     WeekInfo  `json:"week"`
	Enabled  bool      `json:"enabled"`
}

type StatusResult struct {
	Enabled    bool   `json:"enabled"`
	PluginName string `json:"plugin_name"`
	Storage    string `json:"storage"`
}

type ImportResult struct {
	Imported int               `json:"imported"`
	Replaced bool              `json:"replaced"`
	Warnings []string          `json:"warnings,omitempty"`
	Schedule *ScheduleResponse `json:"schedule"`
}
