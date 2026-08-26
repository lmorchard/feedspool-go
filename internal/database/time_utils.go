package database

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type nullableTimeDestination struct {
	destination *time.Time
}

func scanNullableTime(destination *time.Time) sql.Scanner {
	return &nullableTimeDestination{destination: destination}
}

func (scanner *nullableTimeDestination) Scan(value interface{}) error {
	*scanner.destination = time.Time{}
	if value == nil {
		return nil
	}
	timestamp, err := parseDatabaseTime(value)
	if err != nil {
		return err
	}
	*scanner.destination = timestamp
	return nil
}

func parseDatabaseTime(value interface{}) (time.Time, error) {
	if timestamp, ok := value.(time.Time); ok {
		return timestamp, nil
	}

	var text string
	switch value := value.(type) {
	case string:
		text = value
	case []byte:
		text = string(value)
	default:
		var nullable sql.NullTime
		if err := nullable.Scan(value); err != nil {
			return time.Time{}, err
		}
		return nullable.Time, nil
	}

	if monotonic := strings.Index(text, " m="); monotonic >= 0 {
		text = text[:monotonic]
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02",
	} {
		if timestamp, err := time.Parse(layout, text); err == nil {
			return timestamp, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported database time %q", text)
}

func formatDatabaseTime(timestamp time.Time) string {
	return timestamp.UTC().Format(time.RFC3339Nano)
}

// ParseTimeWindow parses CLI time arguments and returns start and end times.
func ParseTimeWindow(maxAge, startStr, endStr string) (startTime, endTime time.Time, err error) {
	// Parse explicit time range first
	if startStr != "" || endStr != "" {
		return parseExplicitTimeRange(startStr, endStr)
	}

	// Parse max age duration
	if maxAge != "" {
		return parseMaxAgeDuration(maxAge)
	}

	// Default to 24 hours if nothing specified
	endTime = time.Now().UTC()
	startTime = endTime.Add(-24 * time.Hour)
	return startTime, endTime, nil
}

func parseExplicitTimeRange(startStr, endStr string) (startTime, endTime time.Time, err error) {
	if startStr != "" {
		startTime, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start time format: %w", err)
		}
		startTime = startTime.UTC()
	}

	if endStr != "" {
		endTime, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end time format: %w", err)
		}
		endTime = endTime.UTC()
	} else {
		endTime = time.Now().UTC()
	}

	if !startTime.IsZero() && !endTime.IsZero() && startTime.After(endTime) {
		return time.Time{}, time.Time{}, fmt.Errorf("start time cannot be after end time")
	}

	return startTime, endTime, nil
}

func parseMaxAgeDuration(maxAge string) (startTime, endTime time.Time, err error) {
	duration, err := ParseDuration(maxAge)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid max-age duration: %w", err)
	}

	endTime = time.Now().UTC()
	startTime = endTime.Add(-duration)
	return startTime, endTime, nil
}

// ParseDuration parses duration strings including "d" for days and "w" for weeks.
func ParseDuration(s string) (time.Duration, error) {
	re := regexp.MustCompile(`^(\d+)([dwh])$`)
	matches := re.FindStringSubmatch(strings.ToLower(s))

	if len(matches) != 3 {
		return time.ParseDuration(s)
	}

	num, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, err
	}

	switch matches[2] {
	case "d":
		return time.Duration(num) * 24 * time.Hour, nil
	case "w":
		return time.Duration(num) * 7 * 24 * time.Hour, nil
	case "h":
		return time.Duration(num) * time.Hour, nil
	default:
		return time.ParseDuration(s)
	}
}
