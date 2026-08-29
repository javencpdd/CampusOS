package schedule

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

// HistoricalSchedule is a validated legacy JSON candidate. SourcePath is
// relative to the requested root so reports are portable and never leak a
// host absolute path.
type HistoricalSchedule struct {
	OwnerID        string    `json:"owner_id"`
	TermYear       int       `json:"term_year"`
	Semester       string    `json:"semester"`
	FirstWeekStart string    `json:"first_week_start"`
	SHA256         string    `json:"sha256"`
	SizeBytes      int64     `json:"size_bytes"`
	SourcePath     string    `json:"source_path"`
	ActiveInIndex  bool      `json:"active_in_index"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type HistoricalScheduleIssue struct {
	Kind       string `json:"kind"`
	OwnerID    string `json:"owner_id,omitempty"`
	SourcePath string `json:"source_path,omitempty"`
}

type HistoricalScheduleReport struct {
	Root       string                    `json:"root"`
	ScannedAt  time.Time                 `json:"scanned_at"`
	Candidates []HistoricalSchedule      `json:"candidates"`
	Issues     []HistoricalScheduleIssue `json:"issues"`
	rootPath   string
}

// SourceRoot is intentionally not serialized. Command code needs it when an
// explicitly authorized adoption copies a validated source file, while a
// portable dry-run report must never reveal an absolute host path.
func (r HistoricalScheduleReport) SourceRoot() string { return r.rootPath }

// ScanHistoricalSchedules implements the dry-run half of v14 schedule
// adoption. It never modifies JSON, never follows symlinks, and refuses a
// candidate whose owner, filename, term, first-week date, or hash cannot be
// proven locally.
func ScanHistoricalSchedules(root string, now time.Time) (HistoricalScheduleReport, error) {
	cleanRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return HistoricalScheduleReport{}, err
	}
	report := HistoricalScheduleReport{Root: filepath.Base(cleanRoot), ScannedAt: now.UTC(), Candidates: []HistoricalSchedule{}, Issues: []HistoricalScheduleIssue{}, rootPath: cleanRoot}
	addIssue := func(kind, owner, path string) {
		report.Issues = append(report.Issues, HistoricalScheduleIssue{Kind: kind, OwnerID: owner, SourcePath: filepath.ToSlash(path)})
	}
	entries, err := os.ReadDir(cleanRoot)
	if errors.Is(err, os.ErrNotExist) {
		return report, nil
	}
	if err != nil {
		return HistoricalScheduleReport{}, err
	}
	for _, userEntry := range entries {
		if userEntry.Type()&os.ModeSymlink != 0 {
			addIssue("unsafe_symlink", "", userEntry.Name())
			continue
		}
		if !userEntry.IsDir() || !corestorage.SafeSegment(userEntry.Name()) {
			continue
		}
		owner := userEntry.Name()
		termsRoot := filepath.Join(cleanRoot, owner, corestorage.FileDir, "schedule", "terms")
		termEntries, readErr := os.ReadDir(termsRoot)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return HistoricalScheduleReport{}, readErr
		}
		activeKey, activeIssue := historicalActiveKey(filepath.Join(cleanRoot, owner, corestorage.FileDir, "schedule", "index.json"), owner)
		if activeIssue != "" {
			addIssue(activeIssue, owner, filepath.Join(owner, corestorage.FileDir, "schedule", "index.json"))
		}
		for _, entry := range termEntries {
			rel := filepath.Join(owner, corestorage.FileDir, "schedule", "terms", entry.Name())
			if entry.Type()&os.ModeSymlink != 0 {
				addIssue("unsafe_symlink", owner, rel)
				continue
			}
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
				continue
			}
			candidate, issue := parseHistoricalSchedule(filepath.Join(termsRoot, entry.Name()), rel, owner)
			if issue != "" {
				addIssue(issue, owner, rel)
				continue
			}
			candidate.ActiveInIndex = activeKey != "" && activeKey == termKey(candidate.TermYear, candidate.Semester)
			report.Candidates = append(report.Candidates, candidate)
		}
	}
	sort.Slice(report.Candidates, func(i, j int) bool {
		if report.Candidates[i].OwnerID != report.Candidates[j].OwnerID {
			return report.Candidates[i].OwnerID < report.Candidates[j].OwnerID
		}
		if report.Candidates[i].TermYear != report.Candidates[j].TermYear {
			return report.Candidates[i].TermYear < report.Candidates[j].TermYear
		}
		return report.Candidates[i].Semester < report.Candidates[j].Semester
	})
	sort.Slice(report.Issues, func(i, j int) bool {
		if report.Issues[i].SourcePath != report.Issues[j].SourcePath {
			return report.Issues[i].SourcePath < report.Issues[j].SourcePath
		}
		return report.Issues[i].Kind < report.Issues[j].Kind
	})
	return report, nil
}

func parseHistoricalSchedule(path, relativePath, owner string) (HistoricalSchedule, string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return HistoricalSchedule{}, "unreadable_json"
	}
	var item Schedule
	if err = json.Unmarshal(raw, &item); err != nil {
		return HistoricalSchedule{}, "invalid_json"
	}
	if item.UserID != "" && item.UserID != owner {
		return HistoricalSchedule{}, "owner_mismatch"
	}
	semester, err := normalizeSemester(item.Semester)
	if err != nil || item.TermYear < 2000 || item.TermYear > 2200 {
		return HistoricalSchedule{}, "invalid_term"
	}
	if strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) != termKey(item.TermYear, semester) {
		return HistoricalSchedule{}, "term_filename_mismatch"
	}
	firstWeek, err := time.Parse(dateLayout, item.FirstWeekStart)
	if err != nil || firstWeek.Weekday() != time.Monday {
		return HistoricalSchedule{}, "invalid_first_week_monday"
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return HistoricalSchedule{}, "unreadable_json"
	}
	sum, err := fileSHA256(path)
	if err != nil {
		return HistoricalSchedule{}, "unreadable_json"
	}
	return HistoricalSchedule{OwnerID: owner, TermYear: item.TermYear, Semester: semester, FirstWeekStart: item.FirstWeekStart, SHA256: sum, SizeBytes: info.Size(), SourcePath: filepath.ToSlash(relativePath), UpdatedAt: item.UpdatedAt.UTC()}, ""
}

func historicalActiveKey(path, owner string) (string, string) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", ""
	}
	if err != nil {
		return "", "invalid_index"
	}
	var index scheduleIndex
	if json.Unmarshal(raw, &index) != nil || (index.UserID != "" && index.UserID != owner) {
		return "", "invalid_index"
	}
	semester, err := normalizeSemester(index.Semester)
	if err != nil || index.TermYear < 2000 || index.TermYear > 2200 {
		return "", "invalid_index"
	}
	return termKey(index.TermYear, semester), ""
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Ensure fs is retained in the source import list on platforms where a
// symlink DirEntry reports an incomplete type bit; it documents that WalkDir
// is intentionally not used for adoption.
var _ fs.FileInfo
