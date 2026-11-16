package domain

import (
	"errors"
	"testing"
	"time"
)

var alice = Author{UserID: "user-alice", Username: "alice"}

// A user suite gets its defaults and its author, and its revision advances only when what runs, or
// who may run it, changes.
func TestUserSuiteRevisions(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	script := "#!/bin/sh\ntest -d /scratch\n"
	suite, err := NewUserSuite("u-1", UserSuiteInput{Name: " scratch ", Script: script}, alice, now)
	if err != nil {
		t.Fatalf("NewUserSuite() error = %v", err)
	}
	if suite.Name != "scratch" || suite.Sudo || suite.StarterLevel != StarterOwner || suite.TimeoutSeconds != 600 ||
		suite.Impact != ImpactWorkload || suite.Revision != 1 || suite.BudgetSeconds != 900 || suite.ScriptSHA256 == "" ||
		len(suite.Checks) != 1 || suite.CreatedBy != "alice" || suite.CreatedByID != "user-alice" {
		t.Errorf("NewUserSuite() = %+v", suite)
	}
	for _, tc := range []struct {
		name         string
		input        UserSuiteInput
		wantRevision int
	}{
		{name: "name and description", input: UserSuiteInput{Name: "scratch mount", Description: "d", Script: script}, wantRevision: 1},
		{name: "script", input: UserSuiteInput{Name: "scratch", Script: "#!/bin/sh\nexit 1\n"}, wantRevision: 2},
		{name: "sudo", input: UserSuiteInput{Name: "scratch", Script: script, Sudo: true}, wantRevision: 2},
		{name: "starter level", input: UserSuiteInput{Name: "scratch", Script: script, StarterLevel: StarterMember}, wantRevision: 2},
		{name: "timeout", input: UserSuiteInput{Name: "scratch", Script: script, TimeoutSeconds: 30}, wantRevision: 2},
		{name: "impact", input: UserSuiteInput{Name: "scratch", Script: script, Impact: ImpactObserve}, wantRevision: 2},
		{name: "defaults spelled out", input: UserSuiteInput{Name: "scratch", Script: script, StarterLevel: StarterOwner,
			TimeoutSeconds: 600, Impact: ImpactWorkload}, wantRevision: 1},
	} {
		next, err := suite.Replace(tc.input, alice.UserID, now)
		if err != nil || next.Revision != tc.wantRevision {
			t.Errorf("Replace(%s) = revision %d, %v, want %d", tc.name, next.Revision, err, tc.wantRevision)
		}
	}
	for name, input := range map[string]UserSuiteInput{
		"no name":       {Script: "x"},
		"no script":     {Name: "a"},
		"bad level":     {Name: "a", Script: "x", StarterLevel: "admin"},
		"long timeout":  {Name: "a", Script: "x", TimeoutSeconds: 7200},
		"unknown scale": {Name: "a", Script: "x", Impact: "huge"},
	} {
		if _, err := NewUserSuite("u", input, alice, now); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("NewUserSuite(%s) error = %v, want ErrInvalidRequest", name, err)
		}
	}
	retired := suite
	retired.Retired = true
	if _, err := retired.Replace(UserSuiteInput{Name: "a", Script: "x"}, alice.UserID, now); !errors.Is(err, ErrSuiteImmutable) {
		t.Errorf("Replace(retired) error = %v, want ErrSuiteImmutable", err)
	}
	record := suite.CurrentRevision(now)
	if record.SuiteID != "u-1" || record.Revision != 1 || record.Script != script || record.ScriptSHA256 != suite.ScriptSHA256 ||
		record.StarterLevel != StarterOwner || !record.CreatedAt.Equal(now) {
		t.Errorf("CurrentRevision() = %+v", record)
	}
}

// Only the author replaces a user suite (an admin included); the author or an admin retires it; a
// suite whose author is unknown has none; a module suite is changed by no one.
func TestUserSuiteAuthorship(t *testing.T) {
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	suite, err := NewUserSuite("u-1", UserSuiteInput{Name: "a", Script: "x"}, alice, now)
	if err != nil {
		t.Fatal(err)
	}
	orphan := suite
	orphan.CreatedByID = ""
	module := Suite{ID: "host-baseline", Source: SourceModule}
	input := UserSuiteInput{Name: "b", Script: "y"}
	for _, tc := range []struct {
		name       string
		suite      Suite
		actorID    string
		admin      bool
		replaceErr error
		retireErr  error
	}{
		{name: "the author", suite: suite, actorID: "user-alice"},
		{name: "another user", suite: suite, actorID: "user-bob", replaceErr: ErrNotSuiteAuthor, retireErr: ErrNotSuiteAuthor},
		{name: "an admin", suite: suite, actorID: "user-root", admin: true, replaceErr: ErrNotSuiteAuthor},
		{name: "a deleted author's suite, by a user", suite: orphan, actorID: "", replaceErr: ErrNotSuiteAuthor, retireErr: ErrNotSuiteAuthor},
		{name: "a deleted author's suite, by an admin", suite: orphan, actorID: "user-root", admin: true, replaceErr: ErrNotSuiteAuthor},
		{name: "a module suite", suite: module, actorID: "user-root", admin: true, replaceErr: ErrSuiteImmutable, retireErr: ErrSuiteImmutable},
	} {
		if _, err := tc.suite.Replace(input, tc.actorID, now); !errors.Is(err, tc.replaceErr) {
			t.Errorf("%s: Replace() error = %v, want %v", tc.name, err, tc.replaceErr)
		}
		if err := tc.suite.CheckRetire(tc.actorID, tc.admin); !errors.Is(err, tc.retireErr) {
			t.Errorf("%s: CheckRetire() error = %v, want %v", tc.name, err, tc.retireErr)
		}
	}
}

// A user suite requires its own starter level; a module suite always requires owner.
func TestRequiredLevel(t *testing.T) {
	member, err := NewUserSuite("u-1", UserSuiteInput{Name: "a", Script: "x", StarterLevel: StarterMember}, alice, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		suite Suite
		want  StarterLevel
	}{
		"member-level user suite": {suite: member, want: StarterMember},
		"default user suite":      {suite: Suite{Source: SourceUser, StarterLevel: StarterOwner}, want: StarterOwner},
		"module suite":            {suite: Suite{Source: SourceModule, StarterLevel: StarterMember}, want: StarterOwner},
	} {
		if got := tc.suite.RequiredLevel(); got != tc.want {
			t.Errorf("%s: RequiredLevel() = %s, want %s", name, got, tc.want)
		}
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
