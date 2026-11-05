package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/maple52046/swallow/internal/adapters/executor/temporalworkflow"
	platformapp "github.com/maple52046/swallow/internal/core/platform/application"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/internal/shared/redact"
	"github.com/maple52046/swallow/internal/shared/sshprobe"
)

// platformUninstallFinalizer clears a Platform's projections after a successful uninstall, and
// removes its record when deleteRecord is set. It is the completion hook for the
// release-and-uninstall path, whose workflow has no ansible step (whose platform-uninstall
// completion does it otherwise), so this narrow port lets the internal complete-uninstall step run the same cleanup
// keyed only by platform id. Implementations must treat an already-deleted Platform as success.
type platformUninstallFinalizer interface {
	CompleteUninstall(ctx context.Context, platformID string, deleteRecord bool) error
}

// platformWorkflowStepExecutor implements swallow-owned readiness and health phases.
// It exposes only normalized outcomes to Temporal; provider and socket details remain in
// the adapters that own them.
type platformWorkflowStepExecutor struct {
	servers            serverdomain.ServerRepository
	configurations     workflowdomain.AutomationConfigurationRepository
	membership         *platformapp.MembershipSyncUseCase
	finalizer          platformUninstallFinalizer
	imageVerifications provisioningdomain.OSImageVerificationRepository
	// software marks Software Assignments installed/absent from the software Workflow's internal
	// record step (decision 038). Nil disables the software record kinds.
	software softwaredomain.AssignmentRepository
	// bootMedia runs the ensure-boot-media Task before an OS deployment (decision 047). Nil fails
	// that Task kind as unsupported.
	bootMedia bootMediaEnsurer
	// bootISOs resolves a Server's chosen Boot ISO for the inspect-hardware ensure-boot-media
	// Task, which reads Boot Media when it runs rather than from a frozen URL (decision 053).
	bootISOs serverdomain.BootISOResolver
	// sshProber performs the authenticated SSH readiness probe for wait-for-ssh. Nil uses the
	// x/crypto/ssh client; tests inject a fake to exercise readiness without a live SSH server.
	sshProber sshprobe.Prober
	// sshAuthGrace overrides how long a reachable-but-rejecting host is tolerated before failing
	// fast on authentication. Zero uses sshAuthGracePeriod; tests set it small.
	sshAuthGrace time.Duration
	// platformHealthWait overrides platformHealthBudget. Zero uses it; tests set it small.
	platformHealthWait time.Duration
	poll               time.Duration
}

func (e platformWorkflowStepExecutor) Execute(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	switch input.Step.Kind {
	case "noop":
		return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskSucceeded, Progress: 100}
	case "wait-for-ssh":
		return e.waitForSSH(ctx, input)
	case "validate-platform-health":
		return e.validatePlatform(ctx, input)
	case "complete-uninstall":
		return e.completeUninstall(ctx, input.PlatformID, input.Step)
	case "record-image-verification":
		return e.recordImageVerification(ctx, input)
	case "record-image-verification-failure":
		return e.recordImageVerificationFailure(ctx, input)
	case "record-software-assignment":
		return e.recordSoftwareAssignment(ctx, input, softwaredomain.StateInstalled)
	case "clear-software-assignment":
		return e.recordSoftwareAssignment(ctx, input, softwaredomain.StateAbsent)
	case ensureBootMediaTaskKind:
		return e.ensureBootMedia(ctx, input)
	default:
		return internalStepFailed("unsupported_internal_step", "The internal Step kind is not supported.", false)
	}
}

// recordImageVerification is the finalize step of a verify-os-image Workflow: after the proving
// deploy succeeded, it writes the swallow-owned attestation that the image works for the deploy
// target. Separated from the deploy so the attestation write can retry without re-deploying. A
// write failure is retryable because the deploy already succeeded; only the record is missing.
func (e platformWorkflowStepExecutor) recordImageVerification(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	if e.imageVerifications == nil {
		return internalStepFailed("image_verification_unavailable", "Image verification recording is unavailable.", false)
	}
	params := input.Step.Parameters
	integrationID, _ := params["integrationId"].(string)
	imageID, _ := params["imageId"].(string)
	architecture, _ := params["architecture"].(string)
	targetRaw, _ := params["deployTarget"].(string)
	target, ok := provisioningdomain.ParseDeployTarget(targetRaw)
	if integrationID == "" || imageID == "" || architecture == "" || !ok {
		return internalStepFailed("image_verification_invalid", "The image verification Step is missing an image identity or deploy target.", false)
	}
	evidence := provisioningdomain.OSImageVerificationEvidence{
		VerifiedAt:  time.Now().UTC(),
		OperationID: input.OperationID,
		ServerID:    firstTargetServer(input.Step),
	}
	if err := e.imageVerifications.RecordTarget(ctx, integrationID, imageID, architecture, target, evidence); err != nil {
		return internalStepFailed("image_verification_write_failed", err.Error(), true)
	}
	return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskSucceeded, Progress: 100}
}

// recordImageVerificationFailure is the failure-path finalize step of a verify-os-image Workflow:
// it runs when the proving deploy failed and records the swallow-owned fact that the image did not
// deploy for the target, so the catalog can show a failed verification distinctly from a
// never-attempted one. The provider's reason lives on the failed provision Task; this records the
// Operation and Server for the user to open, plus an optional short reason. A write failure is
// retryable: the failure fact is worth persisting so the user is not misled into thinking the
// run never happened. This step succeeding does not mean the image is usable — it means the failure
// was recorded — so it returns TaskSucceeded and lets the return-to-ready step run next.
func (e platformWorkflowStepExecutor) recordImageVerificationFailure(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	if e.imageVerifications == nil {
		return internalStepFailed("image_verification_unavailable", "Image verification recording is unavailable.", false)
	}
	params := input.Step.Parameters
	integrationID, _ := params["integrationId"].(string)
	imageID, _ := params["imageId"].(string)
	architecture, _ := params["architecture"].(string)
	targetRaw, _ := params["deployTarget"].(string)
	reason, _ := params["reason"].(string)
	target, ok := provisioningdomain.ParseDeployTarget(targetRaw)
	if integrationID == "" || imageID == "" || architecture == "" || !ok {
		return internalStepFailed("image_verification_invalid", "The image verification Step is missing an image identity or deploy target.", false)
	}
	failure := provisioningdomain.OSImageVerificationFailure{
		FailedAt:    time.Now().UTC(),
		OperationID: input.OperationID,
		ServerID:    firstTargetServer(input.Step),
		Reason:      reason,
	}
	if err := e.imageVerifications.RecordFailedTarget(ctx, integrationID, imageID, architecture, target, failure); err != nil {
		return internalStepFailed("image_verification_write_failed", err.Error(), true)
	}
	return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskSucceeded, Progress: 100}
}

// completeUninstall finalizes a release-and-uninstall Operation by clearing the Platform's
// projections (membership, owned credential Integration, sync), and removing the record when the
// Task's deleteRecord parameter says the user asked for it. It runs after every release
// step succeeds, replacing the ansible step's AnsibleStepSucceeded cleanup that this path
// omits. A missing finalizer or platform id is a non-retryable failure; the cleanup itself is
// idempotent, so a retried step is safe.
func (e platformWorkflowStepExecutor) completeUninstall(ctx context.Context, platformID string, step workflowdomain.Task) temporalworkflow.StepExecutionResult {
	if e.finalizer == nil || strings.TrimSpace(platformID) == "" {
		return internalStepFailed("platform_finalize_unavailable", "Platform uninstall finalization is unavailable.", false)
	}
	// A Workflow accepted before deleteRecord existed has no such parameter and keeps the record.
	deleteRecord, _ := step.Parameters[deleteRecordParameter].(bool)
	if err := e.finalizer.CompleteUninstall(ctx, platformID, deleteRecord); err != nil {
		return internalStepFailed("platform_finalize_failed", err.Error(), true)
	}
	return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskSucceeded, Progress: 100}
}

// recordSoftwareAssignment is the software Workflow's internal record step (decision 038): on a
// successful install it marks each target's Software Assignment installed, and on a successful
// uninstall it marks it absent. It is idempotent — a retried step re-applies the same terminal
// state — and it tolerates a missing assignment (a not-yet-written pending record) as a no-op so a
// benign race cannot fail the run. A write failure is retryable because the software work already
// succeeded; only the record is missing.
func (e platformWorkflowStepExecutor) recordSoftwareAssignment(ctx context.Context, input temporalworkflow.StepExecutionInput, state softwaredomain.AssignmentState) temporalworkflow.StepExecutionResult {
	if e.software == nil {
		return internalStepFailed("software_record_unavailable", "Software assignment recording is unavailable.", false)
	}
	kind, _ := input.Step.Parameters["softwareKind"].(string)
	if kind == "" {
		return internalStepFailed("software_record_invalid", "The software record Step is missing its software kind.", false)
	}
	var appliedAt *time.Time
	if state == softwaredomain.StateInstalled {
		now := time.Now().UTC()
		appliedAt = &now
	}
	for _, serverID := range coerceStringSlice(input.Step.Parameters["serverIds"]) {
		err := e.software.SetState(ctx, serverID, softwaredomain.Kind(kind), state, input.OperationID, appliedAt)
		if err != nil && !errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
			return internalStepFailed("software_record_write_failed", err.Error(), true)
		}
	}
	return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskSucceeded, Progress: 100}
}

// sshAuthGracePeriod is how long a reachable-but-rejecting host is tolerated before wait-for-ssh
// fails fast on authentication. A freshly provisioned host can briefly answer on port 22 before
// cloud-init has written the automation key to authorized_keys, so a transient publickey rejection
// is not treated as fatal immediately; a rejection that persists past this window is a real
// credential problem (the key is not authorized for the SSH user) that will never fix itself by
// waiting, so it is surfaced now instead of at the far slower playbook timeout.
const sshAuthGracePeriod = 45 * time.Second

// waitForSSH blocks until every target host is not just reachable on the SSH port but actually
// accepts the automation key (the Deployment Key, decision 039 — the configurations repository is
// the effective-credential decorator) as the host's login user. Authenticating here
// (rather than only probing TCP) closes the gap where "SSH verified" meant only "port open": a host
// whose login user does not authorize the key now fails at this step with a clear credential
// message, instead of connecting far later in the Ansible Step and failing with a raw
// "Permission denied (publickey)". The login user is resolved per target: the Server's effective
// Server Default User when known (its own value, else the deployed OS Image's default user;
// decision 039), else the candidate list (site user first, then swallow's built-ins), so a
// mixed-image fleet still passes readiness. A rejected key is failed
// fast (after a short grace for cloud-init), while a still-booting host keeps the full readiness
// window. When no automation credential is wired (only in tests), it degrades to a TCP probe.
func (e platformWorkflowStepExecutor) waitForSSH(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	port := 22
	user := ""
	var signer ssh.Signer
	if e.configurations != nil {
		configuration, err := e.configurations.FindBySiteID(ctx, input.SiteID)
		if err != nil {
			return internalStepFailed("ssh_configuration_unavailable", err.Error(), true)
		}
		if configuration.SSHPort > 0 {
			port = configuration.SSHPort
		}
		user = configuration.SSHUser
		credential, err := e.configurations.Credential(ctx, input.SiteID)
		if err != nil {
			return internalStepFailed("ssh_credential_unavailable", err.Error(), true)
		}
		if strings.TrimSpace(credential.SSHPrivateKey) != "" {
			parsed, parseErr := sshprobe.ParseSigner(credential.SSHPrivateKey)
			if parseErr != nil {
				// A malformed key can never authenticate, so waiting is pointless; fail with a clear,
				// non-retryable reason pointing at the automation credential.
				return internalStepFailed("ssh_key_invalid",
					"The Deployment Key's SSH private key could not be parsed: "+parseErr.Error(), false)
			}
			signer = parsed
		}
	}
	// Authenticated readiness needs a usable key; without one (unconfigured automation, reached only
	// in tests) the probe degrades to TCP reachability.
	authenticate := signer != nil
	prober := e.sshProber
	if prober == nil {
		prober = sshprobe.DefaultProber{}
	}

	grace := e.sshAuthGrace
	if grace <= 0 {
		grace = sshAuthGracePeriod
	}
	ticker := time.NewTicker(e.pollInterval())
	defer ticker.Stop()
	deadline := time.NewTimer(20 * time.Minute)
	defer deadline.Stop()

	var firstAuthFailureAt time.Time
	for {
		observation, err := e.probeReadiness(ctx, input.Step, port, signer, prober, user, authenticate)
		if err != nil {
			return internalStepFailed("ssh_readiness_unavailable", err.Error(), true)
		}
		if len(observation.authFailed) > 0 {
			if firstAuthFailureAt.IsZero() {
				firstAuthFailureAt = time.Now()
			}
			// Fail fast once the rejection has outlasted the cloud-init grace: the key is genuinely
			// not authorized for any candidate user, so waiting the full readiness window would only
			// delay the same result.
			if time.Since(firstAuthFailureAt) >= grace {
				failure := internalStepFailed("ssh_authentication_failed", sshAuthFailedMessage(observation.authFailed), true)
				failure.Error.Stage = "ssh_authentication"
				return failure
			}
		} else {
			firstAuthFailureAt = time.Time{}
			if len(observation.missing) == 0 && len(observation.unreachable) == 0 {
				return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskSucceeded, Progress: 100}
			}
		}
		select {
		case <-ctx.Done():
			return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskCanceled}
		case <-deadline.C:
			failure := internalStepFailed("ssh_readiness_timeout", sshReadinessTimeoutMessage(observation, port), true)
			failure.Error.Stage = "ssh_readiness"
			return failure
		case <-ticker.C:
		}
	}
}

// readinessObservation buckets one polling pass over the targets so the caller can decide whether to
// keep waiting (missing address or still booting), fail fast (authentication rejected), or succeed.
// authFailed entries name the host together with the login users tried on it, because each target
// resolves its own candidates.
type readinessObservation struct {
	missing     []string
	unreachable []string
	authFailed  []string
}

// probeReadiness classifies every target in one pass. It reads the latest Server projection for each
// target (so a target that became deployed since the last pass is now probed, and its freshly
// mirrored image default user is used) and then, for a deployed target with an address, resolves
// the SSH login user from that target's candidates. Calls are bounded to eight concurrent probes; a
// repository failure aborts the pass rather than being misreported as a host failure. When
// authenticate is false the probe degrades to TCP reachability and never reports an auth failure.
func (e platformWorkflowStepExecutor) probeReadiness(
	ctx context.Context,
	step workflowdomain.Task,
	port int,
	signer ssh.Signer,
	prober sshprobe.Prober,
	siteUser string,
	authenticate bool,
) (readinessObservation, error) {
	// bucket labels: "ready" needs no tracking; the rest map to the observation fields.
	type probe struct {
		name   string
		bucket string
		err    error
	}
	results := make(chan probe, len(step.Targets))
	semaphore := make(chan struct{}, 8)
	var wait sync.WaitGroup
	for _, target := range step.Targets {
		if target.Kind != "server" {
			continue
		}
		server, err := e.servers.FindByID(ctx, target.ID)
		if err != nil {
			return readinessObservation{}, err
		}
		name := server.DisplayName()
		address := server.PrimaryAddress()
		if address == "" {
			results <- probe{name: name, bucket: "missing"}
			continue
		}
		if server.Absent || server.Provisioning == nil || server.Provisioning.State != "deployed" {
			results <- probe{name: name, bucket: "unreachable"}
			continue
		}
		defaultUser, _ := server.EffectiveDefaultUser()
		candidates := sshprobe.Candidates(defaultUser, siteUser)
		wait.Add(1)
		go func() {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results <- probe{name: name, err: ctx.Err()}
				return
			}
			if !authenticate {
				probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				connection, dialErr := (&net.Dialer{}).DialContext(probeCtx, "tcp", net.JoinHostPort(address, strconv.Itoa(port)))
				if connection != nil {
					_ = connection.Close()
				}
				if dialErr != nil {
					results <- probe{name: name, bucket: "unreachable"}
				} else {
					results <- probe{name: name, bucket: "ready"}
				}
				return
			}
			// Trying every candidate can take a few handshakes, so allow a wider timeout than a
			// single probe.
			probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			_, outcome := sshprobe.Resolve(probeCtx, prober, address, port, signer, candidates)
			switch outcome {
			case sshprobe.Ready:
				results <- probe{name: name, bucket: "ready"}
			case sshprobe.AuthFailed:
				results <- probe{name: fmt.Sprintf("%s (tried %s)", name, strings.Join(candidates, ", ")), bucket: "authFailed"}
			default:
				results <- probe{name: name, bucket: "unreachable"}
			}
		}()
	}
	wait.Wait()
	close(results)

	observation := readinessObservation{missing: []string{}, unreachable: []string{}, authFailed: []string{}}
	for result := range results {
		if result.err != nil && ctx.Err() != nil {
			return readinessObservation{}, result.err
		}
		switch result.bucket {
		case "ready":
			// Ready targets need no bucket.
		case "authFailed":
			observation.authFailed = append(observation.authFailed, result.name)
		case "missing":
			observation.missing = append(observation.missing, result.name)
		default:
			observation.unreachable = append(observation.unreachable, result.name)
		}
	}
	sort.Strings(observation.missing)
	sort.Strings(observation.unreachable)
	sort.Strings(observation.authFailed)
	return observation, nil
}

// sshAuthFailedMessage explains a fail-fast authentication rejection so the user fixes the
// credential or the image's default user rather than the network: the host is reachable but does
// not authorize the automation key for the login users tried on it.
func sshAuthFailedMessage(authFailed []string) string {
	return fmt.Sprintf(
		"SSH authentication failed for: %s. The host is reachable but rejected the automation SSH key (publickey) for the login users tried. "+
			"Check that the OS Image's default user is correct and that the Deployment Key is authorized for it — "+
			"swallow registers the Deployment Key in key-capable provisioners, so a Server deployed before a key change may need redeploying — then retry this Step.",
		strings.Join(authFailed, ", "))
}

// sshReadinessTimeoutMessage separates missing provider observations from a host that never became
// reachable so users know whether to inspect DHCP/addressing or the host itself. Any host still
// failing authentication at the deadline is reported as a credential problem.
func sshReadinessTimeoutMessage(observation readinessObservation, port int) string {
	parts := make([]string, 0, 3)
	if len(observation.missing) > 0 {
		parts = append(parts, fmt.Sprintf("No provider address was observed after OS deployment for: %s.", strings.Join(observation.missing, ", ")))
	}
	if len(observation.unreachable) > 0 {
		parts = append(parts, fmt.Sprintf("SSH port %d did not become reachable for: %s.", port, strings.Join(observation.unreachable, ", ")))
	}
	if len(observation.authFailed) > 0 {
		parts = append(parts, fmt.Sprintf("SSH authentication was rejected for: %s.", strings.Join(observation.authFailed, ", ")))
	}
	return strings.Join(parts, " ")
}

// platformAPIServerParameter names, on a validate-platform-health Task, the Server whose address the
// Platform API endpoint is (the initial controller of a cluster with one control plane). The
// launcher sets it only when the endpoint is that Server's address, not a virtual address, and a
// Task planned before the parameter existed has none; without it the check cannot tell a stale
// endpoint from an API that is down.
const platformAPIServerParameter = "apiServerId"

// platformHealthBudget is how long validate-platform-health waits on a transient condition — the
// Platform API not answering, or the Platform not yet listing every target Server. It is counted
// from the Task's start and shared by both conditions, so alternating between them cannot extend
// it. The Task checks a postcondition (decision 077): the install Task before it already waited
// for the cluster API and for every node to become Ready, so a condition that does not clear
// within the budget is a failure to explain, not progress to wait for.
const platformHealthBudget = 30 * time.Second

// platformHealthMinObservations keeps the budget from ending the wait on a single observation: one
// Platform API call may itself last the Integration's request timeout (30 seconds by default), and
// one call that timed out is no evidence that the API stays unreachable.
const platformHealthMinObservations = 2

// platformHealthCheck is one classified observation of validate-platform-health: what the user
// reads while the Task waits, its Failure Class, and what the Task reports when the wait ends on
// it.
type platformHealthCheck struct {
	healthy     bool
	class       workflowdomain.FailureClass
	code        string
	retryable   bool
	observation string
	remediation string
	cause       error
	// endpoint and operation are the failed Platform API request's, when it recorded them.
	endpoint  string
	operation string
}

// validatePlatform runs validate-platform-health: it syncs the Platform's membership through the
// Platform API until every target Server is a member, publishing each classified observation as
// the Task's wait observation. A permanent condition (classifyPlatformRead) fails the Task at
// once; a transient or unknown one is waited on within platformHealthBudget, after at least
// platformHealthMinObservations observations. Failures stay retryable — what the user fixes (the
// Platform's address, its credential, its nodes) is what a retry checks again — except for a
// Platform that no longer exists or has no reader. A cancel ends the Task as canceled.
func (e platformWorkflowStepExecutor) validatePlatform(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	if strings.TrimSpace(input.PlatformID) == "" || e.membership == nil {
		return internalStepFailed("platform_validation_unavailable", "Platform health validation is unavailable.", false)
	}
	ticker := time.NewTicker(e.pollInterval())
	defer ticker.Stop()
	wait := workflowdomain.NewWaitProgress(time.Now(), e.platformHealthTimeout())
	for {
		check := e.checkPlatformHealth(ctx, input.PlatformID, input.Step)
		if check.healthy {
			return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskSucceeded, Progress: 100}
		}
		if ctx.Err() != nil {
			return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskCanceled}
		}
		now := time.Now()
		waiting := wait.Observe(check.observation, now)
		if check.class == workflowdomain.FailurePermanent ||
			(wait.Exhausted(now) && waiting.Attempts >= platformHealthMinObservations) {
			return e.platformHealthFailed(ctx, input, wait, check, now)
		}
		if publishWaiting(ctx, input, waiting) {
			return supersededResult()
		}
		select {
		case <-ctx.Done():
			return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskCanceled}
		case <-ticker.C:
		}
	}
}

// checkPlatformHealth makes one observation: it syncs the Platform's membership, which records on
// each Server what the Platform API reports, and then reads whether every target Server joined. It
// validates the Workflow's own targets rather than a matched count: a Platform can carry unrelated
// members, and a count can reach the expected total while one target never joined.
func (e platformWorkflowStepExecutor) checkPlatformHealth(ctx context.Context, platformID string, step workflowdomain.Task) platformHealthCheck {
	report, err := e.membership.Execute(ctx, platformID)
	switch {
	case errors.Is(err, platformdomain.ErrPlatformNotFound):
		return platformHealthCheck{
			class: workflowdomain.FailurePermanent, code: "platform_not_found", cause: err,
			observation: "The Platform no longer exists.",
			remediation: "Nothing is left to validate; rerun the deployment to create the Platform again.",
		}
	case err != nil:
		return unclassifiedPlatformRead(err)
	case report.Err != nil:
		return classifyPlatformRead(report.Err)
	case report.Error != nil:
		return classifyPlatformRead(errors.New(*report.Error))
	}
	joined, missing, err := e.targetsJoined(ctx, platformID, step)
	if err != nil {
		check := unclassifiedPlatformRead(err)
		check.observation = "The target Servers' Platform membership could not be read."
		return check
	}
	if len(missing) == 0 {
		return platformHealthCheck{healthy: true}
	}
	return platformHealthCheck{
		class: workflowdomain.FailureTransient, code: "platform_membership_incomplete", retryable: true,
		observation: fmt.Sprintf("Platform membership is missing %d of %d target Servers: %s.",
			len(missing), joined+len(missing), strings.Join(missing, ", ")),
		remediation: "Check that the missing Servers joined the Platform and that the Platform names each node by its Server's hostname, then retry this Task.",
		cause:       fmt.Errorf("the Platform API does not list target Servers %s", strings.Join(missing, ", ")),
	}
}

// classifyPlatformRead classifies a failed membership read. A credential the API rejects, a TLS
// failure, a request the API refuses, and a missing Integration or credential repeat until
// something changes (permanent); a request without an answer, or an API that reports itself
// unavailable, is expected to clear (transient); anything else is unknown and waited on like
// transient.
func classifyPlatformRead(cause error) platformHealthCheck {
	check := platformHealthCheck{class: workflowdomain.FailurePermanent, retryable: true, cause: cause}
	var readerErr *platformdomain.ReaderError
	switch {
	case errors.Is(cause, platformdomain.ErrNoPlatformIntegration), errors.Is(cause, sitedomain.ErrIntegrationNotFound):
		check.code = "platform_integration_missing"
		check.observation = "The Platform has no Integration to read its API through."
		check.remediation = "Redeploy the Platform so it records its Integration."
	case errors.Is(cause, sitedomain.ErrCredentialNotSet):
		check.code = "platform_credential_missing"
		check.observation = "The Platform's Integration holds no credential to read the Platform API with."
		check.remediation = "Redeploy the Platform so it records a read credential."
	case errors.Is(cause, platformdomain.ErrUnsupportedPlatformType):
		check.code, check.retryable = "platform_validation_unavailable", false
		check.observation = "This installation has no reader for the Platform's type."
		check.remediation = "Check that the Platform's module is part of this installation's release."
	case errors.As(cause, &readerErr):
		return classifyReaderError(readerErr, check)
	default:
		return unclassifiedPlatformRead(cause)
	}
	return check
}

// classifyReaderError classifies a Platform API failure by what the API (or the network) answered.
// check carries the cause and the defaults of a permanent failure.
func classifyReaderError(readerErr *platformdomain.ReaderError, check platformHealthCheck) platformHealthCheck {
	check.endpoint, check.operation = readerErr.Endpoint, readerErr.Operation
	api := platformAPIName(readerErr.Endpoint)
	switch {
	case readerErr.Kind == platformdomain.ReaderErrorAuth:
		check.code = "platform_api_auth"
		check.observation = api + " rejected the credential swallow reads it with."
		check.remediation = "Redeploy the Platform so it records a new read credential."
	case readerErr.Transport == platformdomain.TransportTLS:
		check.code = "platform_api_tls"
		check.observation = api + " failed the TLS handshake: its certificate does not match the CA swallow trusts for it."
		check.remediation = "Redeploy the Platform so it records the cluster's current CA, or correct the CA certificate of the Platform's Integration."
	case readerErr.Transport != "" || readerErr.Kind == platformdomain.ReaderErrorUnavailable:
		check.class, check.code = workflowdomain.FailureTransient, "platform_api_unreachable"
		check.observation = unreachableObservation(api, readerErr)
		check.remediation = "Check that the Platform API is running and reachable from swallow's worker, then retry this Task."
	default:
		check.code = "platform_api_rejected"
		check.observation = fmt.Sprintf("%s refused the membership read (%s).", api, readerErr.Kind)
		check.remediation = "Check the Platform API's answer in the cause, then redeploy the Platform."
	}
	return check
}

// unreachableObservation says how a Platform API request went unanswered, in the user's terms.
func unreachableObservation(api string, readerErr *platformdomain.ReaderError) string {
	switch readerErr.Transport {
	case platformdomain.TransportRefused:
		return api + " refused the connection."
	case platformdomain.TransportTimeout:
		return api + " did not answer in time."
	case platformdomain.TransportDNS:
		return api + " could not be reached: its host name did not resolve."
	case "":
		if detail := strings.TrimSpace(readerErr.Detail); detail != "" {
			return api + " reported that it is unavailable: " + detail
		}
		return api + " reported that it is unavailable."
	default:
		return api + " could not be reached."
	}
}

// unclassifiedPlatformRead is the unknown observation of a read that failed for a reason the check
// cannot classify, such as a store error; the cause stays in the diagnostic.
func unclassifiedPlatformRead(cause error) platformHealthCheck {
	return platformHealthCheck{
		class: workflowdomain.FailureUnknown, code: "platform_health_timeout", retryable: true, cause: cause,
		observation: "The Platform's membership could not be read.",
		remediation: "Check the cause, then retry this Task.",
	}
}

// platformHealthFailed is the failed outcome of check at now: its code and observation, and a
// diagnostic with the wait's numbers, the failed request's endpoint and operation, the Platform as
// target, and the remediation. When the endpoint no longer names its API Server's current address
// (staleAPIEndpoint), the message says so and the remediation is to redeploy the Platform.
func (e platformWorkflowStepExecutor) platformHealthFailed(
	ctx context.Context,
	input temporalworkflow.StepExecutionInput,
	wait *workflowdomain.WaitProgress,
	check platformHealthCheck,
	now time.Time,
) temporalworkflow.StepExecutionResult {
	message := check.observation
	diagnostic := wait.Diagnostic(check.class, nil, now)
	diagnostic.Cause = causeChain(check.cause)
	if check.class != workflowdomain.FailurePermanent {
		diagnostic.Cause = workflowdomain.ErrWaitBudgetExhausted.Error() + "; last observation: " + diagnostic.Cause
		message += fmt.Sprintf(" It was still so after %d checks over %d seconds.", diagnostic.Attempts, diagnostic.ElapsedSeconds)
	}
	diagnostic.Target = &workflowdomain.ResourceReference{Kind: "platform", ID: input.PlatformID}
	diagnostic.Endpoint, diagnostic.Operation = check.endpoint, check.operation
	diagnostic.Remediation = check.remediation
	if stale := e.staleAPIEndpoint(ctx, input.Step, check.endpoint); stale != "" {
		message += " " + stale
		diagnostic.Remediation = "Redeploy the Platform so its Integration records the Server's current address."
	}
	failure := internalStepFailed(check.code, message, check.retryable)
	failure.Error.Diagnostic = diagnostic
	return failure
}

// staleAPIEndpoint explains a Platform API endpoint that no longer names the Server it was recorded
// for: the Task's platformAPIServerParameter names that Server, and the endpoint's host is an IP
// address the Server no longer has. It returns "" when it cannot tell (no parameter or endpoint, a
// host name rather than an address, a Server without an observed address) or when the endpoint
// still names one of the Server's addresses.
func (e platformWorkflowStepExecutor) staleAPIEndpoint(ctx context.Context, step workflowdomain.Task, endpoint string) string {
	serverID, _ := step.Parameters[platformAPIServerParameter].(string)
	recorded := net.ParseIP(endpointHost(endpoint))
	if serverID == "" || recorded == nil || e.servers == nil {
		return ""
	}
	server, err := e.servers.FindByID(ctx, serverID)
	if err != nil || server.PrimaryAddress() == "" {
		return ""
	}
	for _, address := range server.Observed.Addresses {
		if current := net.ParseIP(strings.TrimSpace(address)); current != nil && current.Equal(recorded) {
			return ""
		}
	}
	return fmt.Sprintf("The Platform's Integration records its API at %s, but %s, the Server that API runs on, now has address %s: the recorded endpoint is stale.",
		recorded, server.DisplayName(), server.PrimaryAddress())
}

// platformAPIName names the Platform API in an observation, with the scheme and host of the failed
// request when it recorded its endpoint.
func platformAPIName(endpoint string) string {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	switch {
	case strings.TrimSpace(endpoint) == "":
		return "The Platform API"
	case err != nil || parsed.Host == "":
		return "The Platform API at " + redact.URL(endpoint)
	default:
		return "The Platform API at " + parsed.Scheme + "://" + parsed.Host
	}
}

// endpointHost returns the host of an endpoint that is a URL or a host:port, without the port.
func endpointHost(endpoint string) string {
	trimmed := strings.TrimSpace(endpoint)
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" {
		return parsed.Hostname()
	}
	if host, _, err := net.SplitHostPort(trimmed); err == nil {
		return host
	}
	return trimmed
}

func (e platformWorkflowStepExecutor) platformHealthTimeout() time.Duration {
	if e.platformHealthWait > 0 {
		return e.platformHealthWait
	}
	return platformHealthBudget
}

// targetsJoined reports how many of the Operation's target Servers now carry a membership
// projection naming this Platform, and which are still missing. It reads each target's own
// projection (written by the membership sync) rather than trusting an aggregate count.
func (e platformWorkflowStepExecutor) targetsJoined(ctx context.Context, platformID string, step workflowdomain.Task) (int, []string, error) {
	joined := 0
	missing := []string{}
	for _, target := range step.Targets {
		if target.Kind != "server" {
			continue
		}
		server, err := e.servers.FindByID(ctx, target.ID)
		if err != nil {
			return 0, nil, err
		}
		if server.Membership != nil && server.Membership.PlatformID == platformID {
			joined++
		} else {
			missing = append(missing, server.DisplayName())
		}
	}
	sort.Strings(missing)
	return joined, missing, nil
}

func (e platformWorkflowStepExecutor) pollInterval() time.Duration {
	if e.poll <= 0 {
		return 5 * time.Second
	}
	return e.poll
}

func internalStepFailed(code, message string, retryable bool) temporalworkflow.StepExecutionResult {
	return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskFailed, Error: &workflowdomain.NormalizedError{
		Code: code, Message: message, Retryable: retryable,
	}}
}

// causeChain is the redacted text of err for a diagnostic's cause, followed by the innermost error
// it wraps when its own text omits it: a ProviderError prints only its kind and detail, and the
// transport failure under it is the evidence an administrator needs. Only the innermost error is
// added, so a request URL that an intermediate error repeats (with its query) is not.
func causeChain(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	var root error
	for inner := errors.Unwrap(err); inner != nil; inner = errors.Unwrap(inner) {
		root = inner
	}
	if root != nil && !strings.Contains(text, root.Error()) {
		text = strings.TrimSuffix(text, ".") + ": " + root.Error()
	}
	return redact.Text(text)
}

// publishWaiting publishes waiting through the attempt's publisher and reports whether a later
// dispatch superseded the run, in which case the Runner returns supersededResult before its next
// side effect. Any other failure is logged and ignored: the wait observation is advisory and never
// fails the Task (decision 077).
func publishWaiting(ctx context.Context, input temporalworkflow.StepExecutionInput, waiting workflowdomain.TaskWaiting) bool {
	err := input.Waiting.Publish(ctx, waiting)
	if errors.Is(err, workflowdomain.ErrDispatchFenced) {
		return true
	}
	if err != nil && ctx.Err() == nil {
		slog.Warn("publish the Task's wait observation failed",
			"workflowId", input.OperationID, "taskId", input.Step.ID, "error", err)
	}
	return false
}

// supersededCode marks the result of a Runner that stopped because a later dispatch superseded its
// run; it is the code the activity reports for a superseded run.
const supersededCode = "dispatch_fenced"

// supersededResult is what a Runner returns once a later dispatch superseded its run. The core
// refuses to record any outcome for the superseded generation and the superseding run continues the
// Task, so the Runner must not write anything for it either (isSuperseded).
func supersededResult() temporalworkflow.StepExecutionResult {
	return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskCanceled, Error: &workflowdomain.NormalizedError{
		Code: supersededCode, Message: "A later dispatch superseded this run; it continues the Task.",
	}}
}

// isSuperseded reports whether result is supersededResult's.
func isSuperseded(result temporalworkflow.StepExecutionResult) bool {
	return result.Error != nil && result.Error.Code == supersededCode
}
