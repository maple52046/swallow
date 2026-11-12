// Package domain is the Validation bounded context's model (glossary: Validation Suite, Validation
// Check, Validation Run, Validation Verdict, Validation Impact, Validation Schedule; decision 081):
// what a suite declares, what one run asked for and what each of its checks found, how a run's
// verdict follows from its results, and when a schedule is due. It holds no I/O: suites declared by
// modules arrive through the composition root, and runs are executed by Workflows the workflow
// context owns. It must not import an adapter, another context's application layer, or a module.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// Source says who defined a suite.
type Source string

// The suite sources.
const (
	// SourceModule is a suite an Extension Module declares (swallow-defined); it cannot be changed
	// through the API.
	SourceModule Source = "module"
	// SourceUser is a script an admin wrote, judged by its exit code.
	SourceUser Source = "user"
)

// TargetKind is what a suite runs against.
type TargetKind string

// The target kinds.
const (
	// TargetServers is one or more deployed Servers of one Site.
	TargetServers TargetKind = "servers"
	// TargetPlatform is one Platform, entered through its access Servers.
	TargetPlatform TargetKind = "platform"
)

// Impact is the Validation Impact: what a run does to its targets beyond observing them.
type Impact string

// The impacts.
const (
	ImpactObserve    Impact = "observe"
	ImpactWorkload   Impact = "workload"
	ImpactDisruptive Impact = "disruptive"
)

// Valid reports whether i is one of the impacts.
func (i Impact) Valid() bool {
	return i == ImpactObserve || i == ImpactWorkload || i == ImpactDisruptive
}

// RefusesClaimedServers reports whether a Servers suite of this impact must not run on a Server a
// Platform claims: it would compete with the Platform's workloads, whose own suites run through the
// Platform instead.
func (i Impact) RefusesClaimedServers() bool {
	return i == ImpactWorkload || i == ImpactDisruptive
}

// Preemptible reports whether a run of this impact gives way to any Workflow that needs its
// Servers. A disruptive run is not: stopping it midway can leave the target worse off.
func (i Impact) Preemptible() bool {
	return i != ImpactDisruptive
}

// RunAs is the account a user suite's script runs as.
type RunAs string

// The accounts.
const (
	// RunAsAutomationUser runs the script as the account automation logs in as.
	RunAsAutomationUser RunAs = "automation-user"
	// RunAsRoot runs it through become.
	RunAsRoot RunAs = "root"
)

// Check is one Validation Check of a suite.
type Check struct {
	ID          string
	Title       string
	Description string
	// Gating says whether a deployment that composes the suite as a gate fails when the check does
	// not pass.
	Gating bool
}

// ModuleRef names the module that declares a module suite.
type ModuleRef struct {
	Kind    string
	ID      string
	Version string
}

// Suite is a Validation Suite. A module suite carries its Template and Parameters; a user suite its
// script fields. The core never interprets a template or a script: it compiles the one and hands the
// other to the script runner's playbook.
type Suite struct {
	ID          string
	Source      Source
	Module      *ModuleRef
	Name        string
	Description string
	Target      TargetKind
	// PlatformType is the Platform type a platform suite validates; opaque data to the core.
	PlatformType string
	Impact       Impact
	Checks       []Check
	// Parameters is the schema run parameters are checked against; nil accepts only none.
	Parameters *moduledomain.SpecSchema
	// ParametersJSON is that schema as the module declared it, served as written.
	ParametersJSON []byte
	// BudgetSeconds bounds one report run.
	BudgetSeconds int
	// Template runs a module suite.
	Template workflowdomain.Template
	// Experimental marks a module suite offered as a preview whose checks may still change; it runs
	// like any other suite. A user suite is never experimental.
	Experimental bool

	// The user-suite fields.
	Script         string
	RunAs          RunAs
	TimeoutSeconds int
	Revision       int
	ScriptSHA256   string
	Retired        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CreatedBy      string
}

// Check returns the suite's check id, or false.
func (s Suite) Check(id string) (Check, bool) {
	for _, check := range s.Checks {
		if check.ID == id {
			return check, true
		}
	}
	return Check{}, false
}

// ValidateParameters checks run parameters against the suite's schema. A suite without one accepts
// only none.
func (s Suite) ValidateParameters(parameters map[string]any) error {
	if s.Parameters == nil {
		if len(parameters) > 0 {
			return fmt.Errorf("%w: suite %s takes no parameters", ErrInvalidRequest, s.ID)
		}
		return nil
	}
	if err := s.Parameters.Validate(parameters); err != nil {
		return fmt.Errorf("%w: parameters: %w", ErrInvalidRequest, err)
	}
	return nil
}

// The user-suite limits (validation.md §2.2).
const (
	// MaxScriptBytes bounds a user suite's script.
	MaxScriptBytes = 64 * 1024
	// DefaultScriptTimeoutSeconds and MaxScriptTimeoutSeconds bound one script run on one target.
	DefaultScriptTimeoutSeconds = 600
	MaxScriptTimeoutSeconds     = 3600
	// scriptBudgetMarginSeconds is what a user suite's run budget adds to its timeout, for the
	// readiness check and the result recording around the script.
	scriptBudgetMarginSeconds = 300
)

// ScriptCheckID is the one check of a user suite.
const ScriptCheckID = "script"

// UserSuiteInput is what an admin writes for a user suite.
type UserSuiteInput struct {
	Name           string
	Description    string
	Script         string
	RunAs          RunAs
	TimeoutSeconds int
	Impact         Impact
}

// NewUserSuite builds revision 1 of a user suite, applying the defaults (runAs automation-user, a
// 600-second timeout, impact workload). A malformed input is ErrInvalidRequest.
func NewUserSuite(id string, input UserSuiteInput, createdBy string, now time.Time) (Suite, error) {
	suite := Suite{ID: id, Source: SourceUser, Revision: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: createdBy}
	if err := suite.apply(input); err != nil {
		return Suite{}, err
	}
	return suite, nil
}

// Replace returns the suite with input applied; the revision advances when what runs changes (the
// script, its account, its timeout, or its impact). A module suite or a retired one cannot be
// replaced (ErrSuiteImmutable).
func (s Suite) Replace(input UserSuiteInput, now time.Time) (Suite, error) {
	if s.Source != SourceUser || s.Retired {
		return Suite{}, fmt.Errorf("%w: suite %s", ErrSuiteImmutable, s.ID)
	}
	next := s
	if err := next.apply(input); err != nil {
		return Suite{}, err
	}
	if next.ScriptSHA256 != s.ScriptSHA256 || next.RunAs != s.RunAs || next.TimeoutSeconds != s.TimeoutSeconds || next.Impact != s.Impact {
		next.Revision = s.Revision + 1
	}
	next.UpdatedAt = now
	return next, nil
}

func (s *Suite) apply(input UserSuiteInput) error {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(input.Script) == "" {
		return fmt.Errorf("%w: script is required", ErrInvalidRequest)
	}
	if len(input.Script) > MaxScriptBytes {
		return fmt.Errorf("%w: script is larger than %d bytes", ErrInvalidRequest, MaxScriptBytes)
	}
	runAs := input.RunAs
	if runAs == "" {
		runAs = RunAsAutomationUser
	}
	if runAs != RunAsAutomationUser && runAs != RunAsRoot {
		return fmt.Errorf("%w: runAs must be %q or %q", ErrInvalidRequest, RunAsAutomationUser, RunAsRoot)
	}
	timeout := input.TimeoutSeconds
	if timeout == 0 {
		timeout = DefaultScriptTimeoutSeconds
	}
	if timeout < 1 || timeout > MaxScriptTimeoutSeconds {
		return fmt.Errorf("%w: timeoutSeconds must be between 1 and %d", ErrInvalidRequest, MaxScriptTimeoutSeconds)
	}
	impact := input.Impact
	if impact == "" {
		impact = ImpactWorkload
	}
	if !impact.Valid() {
		return fmt.Errorf("%w: impact must be observe, workload, or disruptive", ErrInvalidRequest)
	}
	digest := sha256.Sum256([]byte(input.Script))
	s.Name, s.Description = name, strings.TrimSpace(input.Description)
	s.Script, s.ScriptSHA256 = input.Script, hex.EncodeToString(digest[:])
	s.RunAs, s.TimeoutSeconds, s.Impact = runAs, timeout, impact
	s.Target = TargetServers
	s.Checks = []Check{{ID: ScriptCheckID, Title: "Script exit code", Description: "The script exits 0."}}
	s.BudgetSeconds = timeout + scriptBudgetMarginSeconds
	return nil
}

var (
	// ErrInvalidRequest means a request is malformed: a value the contract does not accept.
	ErrInvalidRequest = errors.New("invalid validation request")
	// ErrSuiteNotFound means no suite has the id, or a user suite is retired where a live one is
	// needed.
	ErrSuiteNotFound = errors.New("validation suite not found")
	// ErrSuiteImmutable means a module suite or a retired user suite was to be changed.
	ErrSuiteImmutable = errors.New("validation suite cannot be changed")
	// ErrSuiteInUse means a Validation Schedule names the suite being retired.
	ErrSuiteInUse = errors.New("validation suite is used by a schedule")
	// ErrRunNotFound means no run has the id.
	ErrRunNotFound = errors.New("validation run not found")
	// ErrRunNotCancelable means the run already ended or is a gate run.
	ErrRunNotCancelable = errors.New("validation run cannot be canceled")
	// ErrTargetNotRunnable means a target cannot be validated now: a Server that is not deployed or
	// is claimed by a Platform, or a Platform that is not active, of another type, or without an
	// access Server.
	ErrTargetNotRunnable = errors.New("validation target cannot be validated")
	// ErrTargetNotFound means a target Server or Platform does not exist.
	ErrTargetNotFound = errors.New("validation target not found")
	// ErrScheduleNotFound means no schedule has the id.
	ErrScheduleNotFound = errors.New("validation schedule not found")
	// ErrGenerationConflict means a schedule write named a generation that is no longer current.
	ErrGenerationConflict = errors.New("the validation schedule changed since it was read")
	// ErrRunRefused wraps the acceptance error of a run's Workflow that a retry would only repeat (a
	// busy or locked target, automation that cannot run), so a schedule skips the occurrence.
	ErrRunRefused = errors.New("the run's Workflow was refused")
)
