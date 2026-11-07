package publicsite

import (
	"bufio"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const MaxChangelogEntries = 2_000

type ChangelogEntry struct {
	Date       time.Time
	Summary    string
	PullURL    string
	PullNumber int
}

var (
	changelogDate  = regexp.MustCompile(`^## (\d{4}-\d{2}-\d{2})$`)
	changelogEntry = regexp.MustCompile(`^- (.+) \(\[#(\d+)\]\((https://github\.com/kedarvartak/px1/pull/(\d+))\)\)$`)
)

// ParseChangelog reads CHANGELOG.md strictly: dated sections newest first, and
// every entry links exactly one merged pull request whose number matches its
// label. Unrecognised lines inside a section are rejected rather than skipped
// so the published page cannot silently drift from the file.
func ParseChangelog(markdown string) ([]ChangelogEntry, error) {
	var entries []ChangelogEntry
	var current time.Time
	seen := map[int]bool{}
	scanner := bufio.NewScanner(strings.NewReader(markdown))
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimRight(scanner.Text(), " \t")
		if match := changelogDate.FindStringSubmatch(line); match != nil {
			date, err := time.Parse(time.DateOnly, match[1])
			if err != nil {
				return nil, fmt.Errorf("line %d: invalid date: %w", lineNumber, err)
			}
			if !current.IsZero() && !date.Before(current) {
				return nil, fmt.Errorf("line %d: sections must be unique and newest first", lineNumber)
			}
			current = date
			continue
		}
		if current.IsZero() || line == "" {
			continue
		}
		match := changelogEntry.FindStringSubmatch(line)
		if match == nil {
			return nil, fmt.Errorf("line %d: entries must be \"- Summary ([#N](pull URL))\"", lineNumber)
		}
		if match[2] != match[4] {
			return nil, fmt.Errorf("line %d: pull request label does not match its link", lineNumber)
		}
		number, _ := strconv.Atoi(match[2])
		if number <= 0 || seen[number] {
			return nil, fmt.Errorf("line %d: pull request #%d is invalid or repeated", lineNumber, number)
		}
		seen[number] = true
		entries = append(entries, ChangelogEntry{Date: current, Summary: match[1], PullURL: match[3], PullNumber: number})
		if len(entries) > MaxChangelogEntries {
			return nil, fmt.Errorf("changelog exceeds %d entries", MaxChangelogEntries)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errors.New("changelog has no entries")
	}
	return entries, nil
}
