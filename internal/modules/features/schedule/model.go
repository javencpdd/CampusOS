package schedule

import "time"

const (
	PluginName     = "personal-schedule"
	SemesterSpring = "spring"
	SemesterFall   = "fall"
)

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
	UserID string `json:"user_id"`
	// AcademicTermID is set by the server for schedules created after v0.14.
	// It is intentionally not accepted from the public write DTO: the active
	// AcademicTerm catalogue, rather than a client supplied ID, owns it.
	AcademicTermID string                 `json:"academic_term_id,omitempty"`
	TermYear       int                    `json:"term_year"`
	Semester       string                 `json:"semester"`
	FirstWeekStart string                 `json:"first_week_start"`
	Settings       Settings               `json:"settings"`
	Courses        []Course               `json:"courses"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

type UpsertRequest struct {
	TermYear       int                    `json:"term_year"`
	Semester       string                 `json:"semester"`
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

type TermSummary struct {
	TermYear       int       `json:"term_year"`
	Semester       string    `json:"semester"`
	FirstWeekStart string    `json:"first_week_start"`
	CourseCount    int       `json:"course_count"`
	UpdatedAt      time.Time `json:"updated_at"`
	Active         bool      `json:"active"`
}

type TermsResponse struct {
	Items []TermSummary `json:"items"`
}

type ActivateTermRequest struct {
	TermYear int    `json:"term_year"`
	Semester string `json:"semester"`
}

type scheduleIndex struct {
	UserID    string    `json:"user_id"`
	TermYear  int       `json:"term_year"`
	Semester  string    `json:"semester"`
	UpdatedAt time.Time `json:"updated_at"`
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
