package schedule

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/extrame/xls"
)

func ParseImport(filename string, data []byte) ([]Course, []string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".xls":
		return parseXLS(filename, data)
	case ".csv":
		return parseCSV(filename, data)
	case ".json":
		return parseJSONImport(filename, data)
	default:
		return nil, nil, fmt.Errorf("%w: %s", ErrUnsupported, ext)
	}
}

func parseXLS(filename string, data []byte) ([]Course, []string, error) {
	workbook, err := xls.OpenReader(bytes.NewReader(data), "utf-8")
	if err != nil {
		return nil, nil, fmt.Errorf("%w: read xls: %v", ErrInvalidInput, err)
	}
	if workbook.NumSheets() == 0 {
		return nil, nil, fmt.Errorf("%w: xls has no sheets", ErrInvalidInput)
	}
	sheet := workbook.GetSheet(0)
	if sheet == nil {
		return nil, nil, fmt.Errorf("%w: first sheet is empty", ErrInvalidInput)
	}
	headerRow := sheet.Row(0)
	if headerRow == nil {
		return nil, nil, fmt.Errorf("%w: header row is empty", ErrInvalidInput)
	}
	headers := mapHeaders(func(index int) string { return headerRow.Col(index) }, headerRow.FirstCol(), headerRow.LastCol())
	var courses []Course
	var warnings []string
	for rowIndex := 1; rowIndex <= int(sheet.MaxRow); rowIndex++ {
		row := sheet.Row(rowIndex)
		if row == nil {
			continue
		}
		course, err := courseFromColumns("xls:"+filename, rowIndex+1, headers, func(index int) string { return row.Col(index) })
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		courses = append(courses, course)
	}
	if len(courses) == 0 {
		return nil, warnings, fmt.Errorf("%w: no courses parsed", ErrInvalidInput)
	}
	return courses, warnings, nil
}

func parseCSV(filename string, data []byte) ([]Course, []string, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.TrimLeadingSpace = true
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: read csv: %v", ErrInvalidInput, err)
	}
	if len(rows) < 2 {
		return nil, nil, fmt.Errorf("%w: csv must contain header and rows", ErrInvalidInput)
	}
	headers := mapHeaders(func(index int) string {
		if index >= len(rows[0]) {
			return ""
		}
		return rows[0][index]
	}, 0, len(rows[0]))
	var courses []Course
	var warnings []string
	for rowIndex := 1; rowIndex < len(rows); rowIndex++ {
		row := rows[rowIndex]
		course, err := courseFromColumns("csv:"+filename, rowIndex+1, headers, func(index int) string {
			if index >= len(row) {
				return ""
			}
			return row[index]
		})
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		courses = append(courses, course)
	}
	if len(courses) == 0 {
		return nil, warnings, fmt.Errorf("%w: no courses parsed", ErrInvalidInput)
	}
	return courses, warnings, nil
}

func parseJSONImport(filename string, data []byte) ([]Course, []string, error) {
	var schedule Schedule
	if err := json.Unmarshal(data, &schedule); err == nil && len(schedule.Courses) > 0 {
		for i := range schedule.Courses {
			schedule.Courses[i].Source = "json:" + filename
		}
		return schedule.Courses, nil, nil
	}
	var courses []Course
	if err := json.Unmarshal(data, &courses); err != nil {
		return nil, nil, fmt.Errorf("%w: json must be a schedule or course array", ErrInvalidInput)
	}
	for i := range courses {
		courses[i].Source = "json:" + filename
	}
	if len(courses) == 0 {
		return nil, nil, fmt.Errorf("%w: no courses parsed", ErrInvalidInput)
	}
	return courses, nil, nil
}

func mapHeaders(valueAt func(int) string, start, end int) map[string]int {
	headers := map[string]int{}
	for index := start; index < end; index++ {
		name := normalizeHeader(valueAt(index))
		if name == "" {
			continue
		}
		headers[name] = index
	}
	return headers
}

func courseFromColumns(source string, rowNumber int, headers map[string]int, valueAt func(int) string) (Course, error) {
	value := func(names ...string) string {
		for _, name := range names {
			if index, ok := headers[normalizeHeader(name)]; ok {
				return strings.TrimSpace(valueAt(index))
			}
		}
		return ""
	}

	name := value("课程名称", "课程", "name", "course")
	if name == "" {
		return Course{}, fmt.Errorf("第 %d 行缺少课程名称", rowNumber)
	}
	timeText := value("时间", "上课时间", "time")
	weekday, start, end, err := parseTimeSlot(timeText)
	if err != nil {
		return Course{}, fmt.Errorf("第 %d 行%s", rowNumber, err.Error())
	}
	weeks := weeksFromRange(value("开始周", "start_week"), value("结束周", "end_week"))
	if len(weeks) == 0 {
		weeks = parseWeeksText(value("周次", "weeks"))
	}
	course := Course{
		Code:        value("课程编号", "编号", "code"),
		Name:        name,
		Teacher:     value("任课教师", "教师", "teacher"),
		Location:    value("上课地点", "地点", "location"),
		Weekday:     weekday,
		StartPeriod: start,
		EndPeriod:   end,
		Weeks:       weeks,
		Source:      source,
		Extra:       map[string]string{},
	}
	for name, index := range headers {
		if _, known := knownHeaders[name]; known {
			continue
		}
		if extra := strings.TrimSpace(valueAt(index)); extra != "" {
			course.Extra[name] = extra
		}
	}
	if len(course.Extra) == 0 {
		course.Extra = nil
	}
	return course, nil
}

var knownHeaders = map[string]struct{}{
	normalizeHeader("课程编号"):       {},
	normalizeHeader("编号"):         {},
	normalizeHeader("code"):       {},
	normalizeHeader("课程名称"):       {},
	normalizeHeader("课程"):         {},
	normalizeHeader("name"):       {},
	normalizeHeader("course"):     {},
	normalizeHeader("任课教师"):       {},
	normalizeHeader("教师"):         {},
	normalizeHeader("teacher"):    {},
	normalizeHeader("开始周"):        {},
	normalizeHeader("start_week"): {},
	normalizeHeader("结束周"):        {},
	normalizeHeader("end_week"):   {},
	normalizeHeader("时间"):         {},
	normalizeHeader("上课时间"):       {},
	normalizeHeader("time"):       {},
	normalizeHeader("上课地点"):       {},
	normalizeHeader("地点"):         {},
	normalizeHeader("location"):   {},
	normalizeHeader("周次"):         {},
	normalizeHeader("weeks"):      {},
}

func normalizeHeader(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "", "\t", "", "-", "_")
	return replacer.Replace(value)
}

var numberPattern = regexp.MustCompile(`\d+`)

func parseTimeSlot(value string) (int, int, int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, 0, 0, fmt.Errorf("缺少上课时间")
	}
	weekday := parseWeekday(value)
	if weekday == 0 {
		return 0, 0, 0, fmt.Errorf("无法识别星期")
	}
	numbers := numberPattern.FindAllString(value, -1)
	if len(numbers) == 0 {
		return 0, 0, 0, fmt.Errorf("无法识别节次")
	}
	start, _ := strconv.Atoi(numbers[0])
	end := start
	if len(numbers) > 1 {
		end, _ = strconv.Atoi(numbers[len(numbers)-1])
	}
	if start <= 0 || end < start {
		return 0, 0, 0, fmt.Errorf("节次范围无效")
	}
	return weekday, start, end, nil
}

func parseWeekday(value string) int {
	mapping := map[string]int{
		"星期一": 1, "周一": 1, "monday": 1, "mon": 1,
		"星期二": 2, "周二": 2, "tuesday": 2, "tue": 2,
		"星期三": 3, "周三": 3, "wednesday": 3, "wed": 3,
		"星期四": 4, "周四": 4, "thursday": 4, "thu": 4,
		"星期五": 5, "周五": 5, "friday": 5, "fri": 5,
		"星期六": 6, "周六": 6, "saturday": 6, "sat": 6,
		"星期日": 7, "星期天": 7, "周日": 7, "周天": 7, "sunday": 7, "sun": 7,
	}
	lower := strings.ToLower(value)
	for key, weekday := range mapping {
		if strings.Contains(lower, key) {
			return weekday
		}
	}
	return 0
}

func weeksFromRange(startText, endText string) []int {
	start, err := strconv.Atoi(strings.TrimSpace(startText))
	if err != nil {
		return nil
	}
	end, err := strconv.Atoi(strings.TrimSpace(endText))
	if err != nil {
		end = start
	}
	if start <= 0 || end < start {
		return nil
	}
	weeks := make([]int, 0, end-start+1)
	for week := start; week <= end && week <= 60; week++ {
		weeks = append(weeks, week)
	}
	return weeks
}

func parseWeeksText(value string) []int {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := regexp.MustCompile(`[,\s，、]+`).Split(value, -1)
	var weeks []int
	for _, part := range parts {
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			weeks = append(weeks, weeksFromRange(bounds[0], bounds[1])...)
			continue
		}
		week, err := strconv.Atoi(part)
		if err == nil {
			weeks = append(weeks, week)
		}
	}
	return normalizeWeeks(weeks)
}
