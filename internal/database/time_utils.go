package database

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

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
	}

	if endStr != "" {
		endTime, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end time format: %w", err)
		}
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
