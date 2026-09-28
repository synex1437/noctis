//go:build !windows

package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// r1ZoneFile writes a zone file (TZif) for a zone offset seconds east of UTC all year round:
// TZ set to its path gives a process that zone as its local time, whatever the system's is.
func r1ZoneFile(t *testing.T, offset int) string {
	t.Helper()
	data := append([]byte("TZif"), make([]byte, 16)...)
	for _, count := range []uint32{0, 0, 0, 0, 1, 4} {
		data = binary.BigEndian.AppendUint32(data, count)
	}
	data = binary.BigEndian.AppendUint32(data, uint32(int32(offset)))
	data = append(data, 0, 0)
	data = append(data, "TST\x00"...)
	file := filepath.Join(t.TempDir(), "zone")
	cliWrite(t, file, data)
	return file
}

// r1SystemZone is the zone launchd and the systemd user manager read calendar times in: the
// system's, from /etc/localtime, UTC without one.
func r1SystemZone(t *testing.T) *time.Location {
	t.Helper()
	data, err := os.ReadFile("/etc/localtime")
	if err != nil {
		return time.UTC
	}
	zone, err := time.LoadLocationFromTZData("system", data)
	if err != nil {
		t.Fatalf("/etc/localtime does not read as a zone: %v", err)
	}
	return zone
}

var r1CalendarField = regexp.MustCompile(`<key>(Month|Day|Hour|Minute)</key><integer>(\d+)</integer>`)

func TestAScheduledResumeFiresAtItsTimeWhateverTZTheProcessThatSchedulesItHas(t *testing.T) {
	const at = 1790000000
	system := r1SystemZone(t)
	_, systemOffset := time.Unix(at, 0).In(system).Zone()
	offset := systemOffset - 19800
	if systemOffset < 0 {
		offset = systemOffset + 19800
	}
	env := map[string]string{"TZ": r1ZoneFile(t, offset)}
	preview := func(backend string) string {
		run := startNoctisCLIAt(t, t.TempDir(), "", env, "schedule-preview", "--backend", backend, "--sid", "abc", "--at", strconv.Itoa(at))()
		if run.code != 0 {
			t.Fatalf("the %s preview failed:\n%s", backend, run)
		}
		return run.stdout
	}

	calendar := ""
	for _, word := range shellWords(strings.TrimSpace(preview("systemd"))) {
		if value, found := strings.CutPrefix(word, "--on-calendar="); found {
			calendar = value
		}
	}
	// systemd reads a calendar time without a zone in the system's zone.
	spec, zone := calendar, system
	if value, found := strings.CutSuffix(calendar, " UTC"); found {
		spec, zone = value, time.UTC
	}
	if fires, err := time.ParseInLocation("2006-01-02 15:04:05", spec, zone); err != nil || fires.Unix() != at {
		t.Fatalf("scheduled from a shell whose TZ is %+.1f h off the system's zone, the systemd timer is set for %q, which fires at %v, not at the resume time %v",
			float64(offset-systemOffset)/3600, calendar, fires, time.Unix(at, 0).In(system))
	}

	plist := preview("launchd")
	got := map[string]int{}
	for _, field := range r1CalendarField.FindAllStringSubmatch(plist, -1) {
		got[field[1]], _ = strconv.Atoi(field[2])
	}
	want := time.Unix(at, 0).Add(time.Minute).Truncate(time.Minute).In(system)
	if got["Month"] != int(want.Month()) || got["Day"] != want.Day() || got["Hour"] != want.Hour() || got["Minute"] != want.Minute() {
		t.Fatalf("scheduled from a shell whose TZ is %+.1f h off the system's zone, the launchd job fires at %02d-%02d %02d:%02d in the system's zone, not at the resume time %s",
			float64(offset-systemOffset)/3600, got["Month"], got["Day"], got["Hour"], got["Minute"], want.Format("01-02 15:04"))
	}
}
