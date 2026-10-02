package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	platformapp "github.com/maple52046/swallow/internal/platform/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/sshprobe"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// platformUninstallFinalizer clears a Platform's projections after a successful uninstall.
// It is the completion hook for the release-and-uninstall path, whose workflow has no ansible
// step (the usual AnsibleStepSucceeded trigger), so this narrow port lets the internal
// complete-uninstall step run the same cleanup keyed only by platform id.
type platformUninstallFinalizer interface {
	CompleteUninstall(ctx context.Context, platformID string) error
}

// platformWorkflowStepExecutor implements Swallow-owned readiness and health phases.
// It exposes only normalized outcomes to Temporal; provider and socket details remain in
// the adapters that own them.
type platformWorkflowStepExecutor struct {
	servers            serverdomain.ServerRepository
	configurations     operationdomain.AutomationConfigurationRepository
	membership         *platformapp.MembershipSyncUseCase
	finalizer          platformUninstallFinalizer
	imageVerifications provisioningdomain.OSImageVerificationRepository
	// software marks Software Assignments installed/absent from the software Workflow's internal
	// record step (decision 038). Nil disables the software record kinds.
	software softwaredomain.AssignmentRepository
	// sshProber performs the authenticated SSH readiness probe for wait-for-ssh. Nil uses the
	// x/crypto/ssh client; tests inject a fake to exercise readiness without a live SSH server.
	sshProber sshprobe.Prober
	// sshAuthGrace overrides how long a reachable-but-rejecting host is tolerated before failing
	// fast on authentication. Zero uses sshAuthGracePeriod; tests set it small.
	sshAuthGrace time.Duration
	poll         time.Duration
}

func (e platformWorkflowStepExecutor) Execute(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	switch input.Step.Kind {
	case "noop":
		return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
	case "wait-for-ssh":
		return e.waitForSSH(ctx, input)
	case "validate-platform-health":
		return e.validatePlatform(ctx, input.PlatformID, input.Step)
	case "complete-uninstall":
		return e.completeUninstall(ctx, input.PlatformID)
	case "record-image-verification":
		return e.recordImageVerification(ctx, input)
	case "record-image-verification-failure":
		return e.recordImageVerificationFailure(ctx, input)
	case "record-software-assignment":
		return e.recordSoftwareAssignment(ctx, input, softwaredomain.StateInstalled)
	case "clear-software-assignment":
		return e.recordSoftwareAssignment(ctx, input, softwaredomain.StateAbsent)
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
	return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
}

// recordImageVerificationFailure is the failure-path finalize step of a verify-os-image Workflow:
// it runs when the proving deploy failed and records the swallow-owned fact that the image did not
// deploy for the target, so the catalog can show a failed verification distinctly from a
// never-attempted one. The provider's reason lives on the failed provision Task; this records the
// Operation and Server for the operator to open, plus an optional short reason. A write failure is
// retryable: the failure fact is worth persisting so the operator is not misled into thinking the
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
	return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
}

// completeUninstall finalizes a release-and-uninstall Operation by clearing the Platform's
// projections (membership, owned credential Integration, sync). It runs after every release
// step succeeds, replacing the ansible step's AnsibleStepSucceeded cleanup that this path
// omits. A missing finalizer or platform id is a non-retryable failure; the cleanup itself is
// idempotent, so a retried step is safe.
func (e platformWorkflowStepExecutor) completeUninstall(ctx context.Context, platformID string) temporalworkflow.StepExecutionResult {
	if e.finalizer == nil || strings.TrimSpace(platformID) == "" {
		return internalStepFailed("platform_finalize_unavailable", "Platform uninstall finalization is unavailable.", false)
	}
	if err := e.finalizer.CompleteUninstall(ctx, platformID); err != nil {
		return internalStepFailed("platform_finalize_failed", err.Error(), true)
	}
	return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
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
	return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
}

// sshAuthGracePeriod is how long a reachable-but-rejecting host is tolerated before wait-for-ssh
// fails fast on authentication. A freshly provisioned host can briefly answer on port 22 before
// cloud-init has written the automation key to authorized_keys, so a transient publickey rejection
// is not treated as fatal immediately; a rejection that persists past this window is a real
// credential problem (the key is not authorized for the SSH user) that will never fix itself by
// waiting, so it is surfaced now instead of at the far slower playbook timeout.
const sshAuthGracePeriod = 45 * time.Second

// waitForSSH blocks until every target host is not just reachable on the SSH port but actually
// accepts the automation key (the Deployment Key, decision 041 — the configurations repository is
// the effective-credential decorator) as the host's login user. Authenticating here
// (rather than only probing TCP) closes the gap where "SSH verified" meant only "port open": a host
// whose login user does not authorize the key now fails at this step with a clear credential
// message, instead of connecting far later in the Ansible Step and failing with a raw
// "Permission denied (publickey)". The login user is resolved per target: the Server's effective
// Server Default User when known (its own value, else the deployed OS Image's default user;
// decisions 045 and 039), else the candidate list (site user first, then swallow's built-ins), so a
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
				return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
			}
		}
		select {
		case <-ctx.Done():
			return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskCanceled}
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
	step operationdomain.Task,
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

// sshAuthFailedMessage explains a fail-fast authentication rejection so the operator fixes the
// credential or the image's default user rather than the network: the host is reachable but does
// not authorize the automation key for the login users tried on it.
func sshAuthFailedMessage(authFailed []string) string {
	return fmt.Sprintf(
		"SSH authentication failed for: %s. The host is reachable but rejected the automation SSH key (publickey) for the login users tried. "+
			"Check that the OS Image's default user is correct and that the Deployment Key (or the site's key override) is authorized for it — "+
			"swallow registers the Deployment Key in key-capable provisioners, so a Server deployed before a key change may need redeploying — then retry this Step.",
		strings.Join(authFailed, ", "))
}

// sshReadinessTimeoutMessage separates missing provider observations from a host that never became
// reachable so operators know whether to inspect DHCP/addressing or the host itself. Any host still
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

func (e platformWorkflowStepExecutor) validatePlatform(ctx context.Context, platformID string, step operationdomain.Task) temporalworkflow.StepExecutionResult {
	if strings.TrimSpace(platformID) == "" || e.membership == nil {
		return internalStepFailed("platform_validation_unavailable", "Platform health validation is unavailable.", false)
	}
	ticker := time.NewTicker(e.pollInterval())
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Minute)
	defer deadline.Stop()
	lastReason := "The Platform API has not reported membership yet."
	for {
		report, err := e.membership.Execute(ctx, platformID)
		if err != nil {
			lastReason = err.Error()
		} else if report.Error != nil {
			lastReason = *report.Error
		} else {
			// Validate the Operation's own target Servers, not a global matched count.
			// A platform can carry unrelated members, and a global count can reach the
			// expected total while a specific target never joined; that would falsely
			// report this deployment healthy.
			joined, missing, checkErr := e.targetsJoined(ctx, platformID, step)
			if checkErr != nil {
				lastReason = checkErr.Error()
			} else if len(missing) == 0 {
				return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
			} else {
				lastReason = fmt.Sprintf("Platform membership is missing %d of %d target Servers: %s.",
					len(missing), joined+len(missing), strings.Join(missing, ", "))
			}
		}
		select {
		case <-ctx.Done():
			return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskCanceled}
		case <-deadline.C:
			return internalStepFailed("platform_health_timeout", lastReason, true)
		case <-ticker.C:
		}
	}
}

// targetsJoined reports how many of the Operation's target Servers now carry a membership
// projection naming this Platform, and which are still missing. It reads each target's own
// projection (written by the membership sync) rather than trusting an aggregate count.
func (e platformWorkflowStepExecutor) targetsJoined(ctx context.Context, platformID string, step operationdomain.Task) (int, []string, error) {
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
	return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskFailed, Error: &operationdomain.NormalizedError{
		Code: code, Message: message, Retryable: retryable,
	}}
}
