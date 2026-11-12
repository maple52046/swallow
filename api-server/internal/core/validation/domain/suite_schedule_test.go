package domain

import (
	"errors"
	"testing"
	"time"
)

// A user suite gets its defaults, and its revision advances only when what runs changes.
func TestUserSuiteRevisions(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	suite, err := NewUserSuite("u-1", UserSuiteInput{Name: " scratch ", Script: "#!/bin/sh\ntest -d /scratch\n"}, "admin", now)
	if err != nil {
		t.Fatalf("NewUserSuite() error = %v", err)
	}
	if suite.Name != "scratch" || suite.RunAs != RunAsAutomationUser || suite.TimeoutSeconds != 600 || suite.Impact != ImpactWorkload ||
		suite.Revision != 1 || suite.BudgetSeconds != 900 || suite.ScriptSHA256 == "" || len(suite.Checks) != 1 {
		t.Errorf("NewUserSuite() = %+v", suite)
	}
	renamed, err := suite.Replace(UserSuiteInput{Name: "scratch mount", Script: suite.Script}, now)
	if err != nil || renamed.Revision != 1 {
		t.Errorf("Replace(name only) = revision %d, %v, want 1", renamed.Revision, err)
	}
	edited, err := suite.Replace(UserSuiteInput{Name: "scratch", Script: "#!/bin/sh\nexit 1\n"}, now)
	if err != nil || edited.Revision != 2 || edited.ScriptSHA256 == suite.ScriptSHA256 {
		t.Errorf("Replace(script) = revision %d, %v, want 2 with a new sha256", edited.Revision, err)
	}
	for name, input := range map[string]UserSuiteInput{
		"no name":       {Script: "x"},
		"no script":     {Name: "a"},
		"bad account":   {Name: "a", Script: "x", RunAs: "nobody"},
		"long timeout":  {Name: "a", Script: "x", TimeoutSeconds: 7200},
		"unknown scale": {Name: "a", Script: "x", Impact: "huge"},
	} {
		if _, err := NewUserSuite("u", input, "admin", now); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("NewUserSuite(%s) error = %v, want ErrInvalidRequest", name, err)
		}
	}
	retired := suite
	retired.Retired = true
	if _, err := retired.Replace(UserSuiteInput{Name: "a", Script: "x"}, now); !errors.Is(err, ErrSuiteImmutable) {
		t.Errorf("Replace(retired) error = %v, want ErrSuiteImmutable", err)
	}
}

func TestCronNext(t *testing.T) {
	taipei, _ := time.LoadLocation("Asia/Taipei")
	after := time.Date(2026, 11, 12, 1, 30, 0, 0, time.UTC) // 09:30 in Taipei, a Thursday
	tests := []struct {
		expression, timezone string
		want                 time.Time
	}{
		{"*/15 * * * *", "", time.Date(2026, 11, 12, 1, 45, 0, 0, time.UTC)},
		{"0 2 * * *", "Asia/Taipei", time.Date(2026, 11, 13, 2, 0, 0, 0, taipei).UTC()},
		{"0 9-17/4 * * 1-5", "Asia/Taipei", time.Date(2026, 11, 12, 13, 0, 0, 0, taipei).UTC()},
		{"30 3 1 * *", "", time.Date(2026, 12, 1, 3, 30, 0, 0, time.UTC)},
		{"0 0 * * 0", "", time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)},
		{"0 0 * * 7", "", time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)},
		{"0 0 13 * 5", "", time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range tests {
		cron, err := ParseCron(tc.expression, tc.timezone)
		if err != nil {
			t.Fatalf("ParseCron(%q) error = %v", tc.expression, err)
		}
		if got, ok := cron.Next(after); !ok || !got.Equal(tc.want) {
			t.Errorf("Next(%q in %q) = %s, %v, want %s", tc.expression, tc.timezone, got, ok, tc.want)
		}
	}
	never, _ := ParseCron("0 0 30 2 *", "")
	if _, ok := never.Next(after); ok {
		t.Error("Next(30 February) found a time")
	}
	for _, bad := range []string{"* * * *", "60 * * * *", "* 24 * * *", "*/0 * * * *", "a * * * *", "5-1 * * * *"} {
		if _, err := ParseCron(bad, ""); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("ParseCron(%q) error = %v, want ErrInvalidRequest", bad, err)
		}
	}
	if _, err := ParseCron("* * * * *", "Mars/Olympus"); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("ParseCron(unknown zone) error = %v, want ErrInvalidRequest", err)
	}
}

func TestScheduleSpecNormalize(t *testing.T) {
	spec, err := ScheduleSpec{Name: " nightly ", SuiteID: "host-baseline", Trigger: ScheduleTrigger{Kind: ScheduleCron, Cron: "0 2 * * *"}, Enabled: true}.Normalize()
	if err != nil || spec.Name != "nightly" || spec.Trigger.Timezone != "UTC" || spec.Parameters == nil {
		t.Errorf("Normalize() = %+v, %v", spec, err)
	}
	if next := spec.NextAfter(time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)); next == nil || !next.Equal(time.Date(2026, 11, 13, 2, 0, 0, 0, time.UTC)) {
		t.Errorf("NextAfter() = %v", next)
	}
	spec.Enabled = false
	if next := spec.NextAfter(time.Now()); next != nil {
		t.Errorf("NextAfter(disabled) = %v, want nil", next)
	}
	for name, bad := range map[string]ScheduleSpec{
		"no suite":          {Name: "a", Trigger: ScheduleTrigger{Kind: ScheduleAfterDeployment}},
		"unknown trigger":   {Name: "a", SuiteID: "s", Trigger: ScheduleTrigger{Kind: "hourly"}},
		"cron after deploy": {Name: "a", SuiteID: "s", Trigger: ScheduleTrigger{Kind: ScheduleAfterDeployment, Cron: "* * * * *"}},
		"malformed cron":    {Name: "a", SuiteID: "s", Trigger: ScheduleTrigger{Kind: ScheduleCron, Cron: "daily"}},
	} {
		if _, err := bad.Normalize(); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("Normalize(%s) error = %v, want ErrInvalidRequest", name, err)
		}
	}
}
