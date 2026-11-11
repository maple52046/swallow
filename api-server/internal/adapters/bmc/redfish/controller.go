package redfish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// Controller implements serverdomain.RedfishController over plain HTTP(S) to the BMC. It is
// safe for concurrent use; each call opens its own session.
type Controller struct {
	client *http.Client
	// taskWait bounds how long an asynchronous BMC action (a Redfish Task) may run.
	taskWait time.Duration
	// settleWait bounds how long a change may take to show up in a re-read.
	settleWait time.Duration
	// mountSettle, when non-zero, replaces the BMC Profile's settle: how long a freshly mounted
	// ISO must stay inserted before the host may be powered on, because some BMCs report a mount
	// complete before the virtual CD is ready for the host. Only tests set it. The wait applies
	// only when the call mounted.
	mountSettle time.Duration
	poll        time.Duration
}

// NewController returns a controller with timings suited to real BMCs: the slowest known takes
// up to about a minute to mount an HTTP ISO and a few seconds to enable its remote media.
func NewController() *Controller {
	return &Controller{
		client:     newHTTPClient(),
		taskWait:   3 * time.Minute,
		settleWait: time.Minute,
		poll:       3 * time.Second,
	}
}

// settleFor returns how long a fresh mount on a BMC of profile p must settle.
func (c *Controller) settleFor(p *profile) time.Duration {
	if c.mountSettle > 0 {
		return c.mountSettle
	}
	return p.mountSettle
}

// Probe implements serverdomain.RedfishController. A BMC that does not answer, refuses the
// account, or lacks a needed function is reported in the result; only an ended ctx is an error.
func (c *Controller) Probe(ctx context.Context, endpoint serverdomain.BMCEndpoint) (serverdomain.RedfishCapability, error) {
	capability := serverdomain.RedfishCapability{ProbedAt: time.Now().UTC()}
	s, err := c.open(endpoint)
	if err != nil {
		capability.Support = serverdomain.RedfishUnreachable
		capability.Reason = err.Error()
		return capability, nil
	}
	capability.ServiceRoot = s.base + "/redfish/v1"
	found, err := s.discover(ctx, endpoint)
	if found != nil {
		capability.Profile = found.profile.name
		capability.Vendor = found.root.vendor()
		capability.Product = strings.TrimSpace(found.root.Product)
		capability.RedfishVersion = found.root.RedfishVersion
		if found.manager != nil {
			capability.FirmwareVersion = found.manager.FirmwareVersion
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return capability, ctx.Err()
		}
		capability.Support, capability.Reason = probeFailure(err)
		return capability, nil
	}
	capability.SystemID = found.system.ID
	capability.VirtualMedia = len(found.cds()) > 0
	capability.BootOverrideModes = overrideModes(found.system.Boot)
	switch {
	case !capability.VirtualMedia:
		capability.Support = serverdomain.RedfishUnsupported
		capability.Reason = "The host System offers no virtual CD."
	case len(capability.BootOverrideModes) == 0:
		capability.Support = serverdomain.RedfishUnsupported
		capability.Reason = "The host System offers no boot source override."
	case !found.canTargetCD():
		capability.Support = serverdomain.RedfishUnsupported
		capability.Reason = found.profile.noDirection
	default:
		capability.Support = serverdomain.RedfishSupported
	}
	return capability, nil
}

// probeFailure turns a discovery error into a support value and a user reason: a rejected
// account (401) is unreachable, while a refused request (403) is unsupported with the request and
// the BMC's words, because the account works and only that function is unavailable.
func probeFailure(err error) (serverdomain.RedfishSupport, string) {
	var unsupported *unsupportedError
	if errors.As(err, &unsupported) {
		return serverdomain.RedfishUnsupported, unsupported.reason
	}
	var status *statusError
	switch {
	case errors.As(err, &status) && status.Status == http.StatusUnauthorized:
		return serverdomain.RedfishUnreachable, rejectedAccount
	case errors.As(err, &status) && status.Status == http.StatusForbidden:
		return serverdomain.RedfishUnsupported, status.refusal()
	case errors.As(err, &status) && status.Status == http.StatusNotFound:
		return serverdomain.RedfishUnreachable, "The BMC has no Redfish service."
	default:
		return serverdomain.RedfishUnreachable, "No Redfish service answered: " + err.Error()
	}
}

// ReadBootMedia implements serverdomain.RedfishController.
func (c *Controller) ReadBootMedia(ctx context.Context, endpoint serverdomain.BMCEndpoint, isoURL string) (_ serverdomain.BootMediaState, err error) {
	defer func() { err = atEndpoint(endpoint, err) }()
	s, err := c.open(endpoint)
	if err != nil {
		return serverdomain.BootMediaState{}, &controllerError{sentinel: serverdomain.ErrBMCUnreachable, detail: err.Error(), permanent: true}
	}
	found, err := s.discover(ctx, endpoint)
	if err != nil {
		return serverdomain.BootMediaState{}, discoveryError(err)
	}
	return found.state(isoURL), nil
}

// ApplyBootMedia implements serverdomain.RedfishController.
func (c *Controller) ApplyBootMedia(ctx context.Context, endpoint serverdomain.BMCEndpoint, isoURL string) (_ string, _ serverdomain.BootMediaState, err error) {
	defer func() { err = atEndpoint(endpoint, err) }()
	s, err := c.open(endpoint)
	if err != nil {
		return "", serverdomain.BootMediaState{}, &controllerError{sentinel: serverdomain.ErrBMCUnreachable, detail: err.Error(), permanent: true}
	}
	found, err := s.discover(ctx, endpoint)
	if err != nil {
		return "", serverdomain.BootMediaState{}, discoveryError(err)
	}
	if len(found.cds()) == 0 {
		return "", serverdomain.BootMediaState{}, &controllerError{sentinel: serverdomain.ErrRedfishUnsupported, detail: "The host System offers no virtual CD.", operation: "mount the ISO", permanent: true}
	}
	if found.mediaHolding(isoURL) == nil {
		serverdomain.ReportBootMediaPhase(ctx, serverdomain.BootMediaPhaseMounting, nil)
		if found, err = c.prepareMount(ctx, s, endpoint, found, isoURL); err != nil {
			return "", serverdomain.BootMediaState{}, err
		}
		if found, err = c.mountAndSettle(ctx, s, endpoint, found, isoURL); err != nil {
			return "", serverdomain.BootMediaState{}, err
		}
	}
	serverdomain.ReportBootMediaPhase(ctx, serverdomain.BootMediaPhaseDirecting, nil)
	mode, err := c.setOverride(ctx, s, found)
	if err != nil {
		return "", serverdomain.BootMediaState{}, err
	}
	serverdomain.ReportBootMediaPhase(ctx, serverdomain.BootMediaPhaseVerifying, nil)
	if found, err = s.discover(ctx, endpoint); err != nil {
		return "", serverdomain.BootMediaState{}, discoveryError(err)
	}
	state := found.state(isoURL)
	if !state.Ready() {
		return mode, state, &controllerError{sentinel: serverdomain.ErrBootMediaRejected,
			detail:    fmt.Sprintf("After the change the BMC reports media inserted=%t and boot override %s/%s.", state.MediaInserted, state.OverrideEnabled, state.OverrideTarget),
			operation: "verify the boot media"}
	}
	return mode, state, nil
}

// ClearBootMedia implements serverdomain.RedfishController.
func (c *Controller) ClearBootMedia(ctx context.Context, endpoint serverdomain.BMCEndpoint, isoURL string) (err error) {
	defer func() { err = atEndpoint(endpoint, err) }()
	s, err := c.open(endpoint)
	if err != nil {
		return &controllerError{sentinel: serverdomain.ErrBMCUnreachable, detail: err.Error(), permanent: true}
	}
	found, err := s.discover(ctx, endpoint)
	if err != nil {
		return discoveryError(err)
	}
	for _, media := range found.media {
		if !media.isCD() || !media.inserted() || !found.profile.imageMatches(media.Image, media.ImageName, isoURL) {
			continue
		}
		if err := c.eject(ctx, s, media); err != nil {
			return err
		}
	}
	if found.overrideTargetsCD() && found.system.Boot.BootSourceOverrideEnabled != "Disabled" {
		if err := c.patchBoot(ctx, s, found.system, map[string]any{"BootSourceOverrideEnabled": "Disabled"}); err != nil {
			return err
		}
	}
	return nil
}

// MountBootMedia implements serverdomain.RedfishController: the mount of ApplyBootMedia without
// its settle and without touching the boot direction.
func (c *Controller) MountBootMedia(ctx context.Context, endpoint serverdomain.BMCEndpoint, isoURL string) (_ serverdomain.BootMediaState, err error) {
	defer func() { err = atEndpoint(endpoint, err) }()
	s, err := c.open(endpoint)
	if err != nil {
		return serverdomain.BootMediaState{}, &controllerError{sentinel: serverdomain.ErrBMCUnreachable, detail: err.Error(), permanent: true}
	}
	found, err := s.discover(ctx, endpoint)
	if err != nil {
		return serverdomain.BootMediaState{}, discoveryError(err)
	}
	if len(found.cds()) == 0 {
		return serverdomain.BootMediaState{}, &controllerError{sentinel: serverdomain.ErrRedfishUnsupported, detail: "The host System offers no virtual CD.", operation: "mount the ISO", permanent: true}
	}
	if found.mediaHolding(isoURL) == nil {
		if found, err = c.prepareMount(ctx, s, endpoint, found, isoURL); err != nil {
			return serverdomain.BootMediaState{}, err
		}
		if err := c.insert(ctx, s, found, isoURL); err != nil {
			return serverdomain.BootMediaState{}, err
		}
		if found, err = c.awaitInserted(ctx, s, endpoint, isoURL); err != nil {
			return serverdomain.BootMediaState{}, err
		}
	}
	return found.state(isoURL), nil
}

// prepareMount runs the BMC Profile's preparation for mounting isoURL (see profile.prepareMount)
// and returns a fresh discovery when it changed anything, else found.
func (c *Controller) prepareMount(ctx context.Context, s *session, endpoint serverdomain.BMCEndpoint, found *discovered, isoURL string) (*discovered, error) {
	if found.profile.prepareMount == nil {
		return found, nil
	}
	changed, err := found.profile.prepareMount(ctx, c, s, found, isoURL)
	if err != nil {
		return nil, err
	}
	if !changed {
		return found, nil
	}
	if found, err = s.discover(ctx, endpoint); err != nil {
		return nil, discoveryError(err)
	}
	return found, nil
}

// ResetHost implements serverdomain.RedfishController. It posts ComputerSystem.Reset with
// ForceRestart, or On for a host that is off, and returns once the BMC accepted it; a 202 Task
// is not followed because the host's boot is observed by the provisioner, not here.
func (c *Controller) ResetHost(ctx context.Context, endpoint serverdomain.BMCEndpoint) (err error) {
	defer func() { err = atEndpoint(endpoint, err) }()
	s, err := c.open(endpoint)
	if err != nil {
		return &controllerError{sentinel: serverdomain.ErrBMCUnreachable, detail: err.Error(), permanent: true}
	}
	found, err := s.discover(ctx, endpoint)
	if err != nil {
		return discoveryError(err)
	}
	target := actionTarget(found.system.Actions, "#ComputerSystem.Reset")
	if target == "" {
		return &controllerError{sentinel: serverdomain.ErrRedfishUnsupported, detail: "The host System offers no reset action.", operation: "restart the host", permanent: true}
	}
	resetType := "ForceRestart"
	if strings.EqualFold(found.system.PowerState, "Off") {
		resetType = "On"
	}
	if _, err := s.do(ctx, http.MethodPost, target, map[string]string{"ResetType": resetType}, nil); err != nil {
		return classify(err)
	}
	return nil
}

// open starts a session for endpoint.
func (c *Controller) open(endpoint serverdomain.BMCEndpoint) (*session, error) {
	base, err := serviceBase(endpoint.Address)
	if err != nil {
		return nil, err
	}
	return &session{client: c.client, base: base, username: endpoint.Username, password: endpoint.Password, pause: c.poll}, nil
}

// atEndpoint names the BMC's service root (without userinfo) on a controller error, so a Task
// diagnostic can say which BMC failed; other errors, and an address that is not a host, pass as
// they are.
func atEndpoint(endpoint serverdomain.BMCEndpoint, err error) error {
	var failure *controllerError
	if !errors.As(err, &failure) || failure.endpoint != "" {
		return err
	}
	if base, baseErr := serviceBase(endpoint.Address); baseErr == nil {
		failure.endpoint = base + "/redfish/v1"
	}
	return err
}

// discoveryError classifies a discovery failure for the apply/read paths.
func discoveryError(err error) error {
	var unsupported *unsupportedError
	if errors.As(err, &unsupported) {
		return &controllerError{sentinel: serverdomain.ErrRedfishUnsupported, detail: unsupported.reason, operation: "discover the host System", permanent: true}
	}
	return classify(err)
}

// unsupportedError means the BMC answered but its resource tree lacks what Boot Media needs.
type unsupportedError struct{ reason string }

func (e *unsupportedError) Error() string { return e.reason }

// discovered is one consistent read of the resources Boot Media touches.
type discovered struct {
	root    serviceRoot
	system  computerSystem
	manager *manager
	media   []virtualMedia
	options []bootOption
	// profile is the BMC Profile recognized from what was read; never nil once the service root
	// was read.
	profile *profile
	// direction is the first of the profile's boot strategies that can direct this System's boot
	// at the virtual CD, or nil when none can.
	direction bootDirection
}

// identity returns what the resources read so far say about the BMC.
func (d *discovered) identity() identity {
	id := identity{vendor: d.root.Vendor, oemKeys: oemKeys(d.root.Oem), actions: actionNames(d.system.Actions)}
	if d.manager != nil {
		id.oemKeys = append(id.oemKeys, oemKeys(d.manager.Oem)...)
		id.actions = append(id.actions, actionNames(d.manager.Actions)...)
	}
	return id
}

// maxMembers bounds how many members of one collection swallow reads, so a BMC that lists
// hundreds of Systems or devices cannot turn one probe into hundreds of slow requests.
const maxMembers = 16

// discover reads the service root, picks the host System, and lists its manager, virtual media,
// and UEFI boot options; it recognizes the BMC Profile from the service root and again once the
// System and Manager are read, then binds the profile's boot strategy. A failure to read the
// optional parts (manager, boot options) is ignored because not every BMC offers them; a missing
// System or media collection is not. A partial result returned with an error still names the
// profile when the service root was read.
func (s *session) discover(ctx context.Context, endpoint serverdomain.BMCEndpoint) (*discovered, error) {
	found := &discovered{}
	if err := s.get(ctx, "/redfish/v1/", &found.root); err != nil {
		return nil, err
	}
	found.profile = selectProfile(found.identity())
	if found.root.Systems.ID == "" {
		return found, &unsupportedError{reason: "The Redfish service lists no Systems."}
	}
	var systems collection
	if err := s.get(ctx, found.root.Systems.ID, &systems); err != nil {
		return found, err
	}
	// A System that cannot be read is skipped rather than failing discovery: a GPU server's
	// accelerator baseboard System answers HTTP 500 while the host is powered off, which is exactly
	// when an OS deployment's ensure step runs. Only when no System can be read is it a failure.
	candidates := make([]computerSystem, 0, len(systems.Members))
	var firstErr error
	for i, member := range systems.Members {
		if i >= maxMembers {
			break
		}
		var system computerSystem
		if err := s.get(ctx, member.ID, &system); err != nil {
			if ctx.Err() != nil {
				return found, err
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		candidates = append(candidates, system)
	}
	if len(candidates) == 0 && firstErr != nil {
		return found, firstErr
	}
	system, err := chooseHostSystem(candidates, endpoint.SystemHint, endpoint.HostUUID)
	if err != nil {
		return found, err
	}
	found.system = system
	if len(system.Links.ManagedBy) > 0 {
		var m manager
		if s.get(ctx, system.Links.ManagedBy[0].ID, &m) == nil {
			found.manager = &m
		} else if ctx.Err() != nil {
			return found, ctx.Err()
		}
	}
	found.profile = selectProfile(found.identity())
	mediaLink := ""
	if system.VirtualMedia != nil {
		mediaLink = system.VirtualMedia.ID
	} else if found.manager != nil && found.manager.VirtualMedia != nil {
		mediaLink = found.manager.VirtualMedia.ID
	}
	if mediaLink != "" {
		var devices collection
		if err := s.get(ctx, mediaLink, &devices); err != nil {
			return found, err
		}
		for i, member := range devices.Members {
			if i >= maxMembers {
				break
			}
			var media virtualMedia
			if err := s.get(ctx, member.ID, &media); err != nil {
				// One unreadable device (a USB stick slot, say) must not hide the virtual CDs.
				if ctx.Err() != nil {
					return found, err
				}
				continue
			}
			found.media = append(found.media, media)
		}
	}
	if system.Boot != nil && system.Boot.BootOptions.ID != "" {
		var options collection
		if s.get(ctx, system.Boot.BootOptions.ID, &options) == nil {
			for i, member := range options.Members {
				if i >= maxMembers*2 {
					break
				}
				var option bootOption
				if s.get(ctx, member.ID, &option) == nil {
					found.options = append(found.options, option)
				}
			}
		} else if ctx.Err() != nil {
			return found, ctx.Err()
		}
	}
	if err := found.prepareDirection(ctx, s); err != nil {
		return found, err
	}
	return found, nil
}

// chooseHostSystem picks the ComputerSystem that is the host: the provisioner's System hint,
// else the one whose UUID is the Server's hardware UUID, else the only one with a Boot object.
// GPU servers expose their accelerator baseboard as a second System without Boot, so "the first
// member" would be wrong.
func chooseHostSystem(systems []computerSystem, hint, hostUUID string) (computerSystem, error) {
	if hint != "" {
		for _, system := range systems {
			if strings.EqualFold(system.ID, hint) || strings.HasSuffix(system.ODataID, "/"+hint) {
				return system, nil
			}
		}
	}
	if uuid := normalizeUUID(hostUUID); uuid != "" {
		for _, system := range systems {
			if normalizeUUID(system.UUID) == uuid {
				return system, nil
			}
		}
	}
	var withBoot []computerSystem
	for _, system := range systems {
		if system.Boot != nil {
			withBoot = append(withBoot, system)
		}
	}
	if len(withBoot) == 1 {
		return withBoot[0], nil
	}
	if len(withBoot) == 0 {
		return computerSystem{}, &unsupportedError{reason: "No Redfish System offers boot control."}
	}
	return computerSystem{}, &unsupportedError{reason: "Several Redfish Systems offer boot control and none matches the Server's hardware UUID."}
}

// normalizeUUID lowercases a UUID and drops dashes so firmware formatting differences match.
func normalizeUUID(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
}

// overrideModes returns the non-Disabled override modes the System allows. A System that does
// not advertise allowable values is assumed to support both, as the schema defines them.
func overrideModes(b *boot) []string {
	if b == nil {
		return nil
	}
	if len(b.BootSourceOverrideEnabledAllowed) == 0 {
		return []string{"Once", "Continuous"}
	}
	var modes []string
	for _, wanted := range []string{"Once", "Continuous"} {
		for _, allowed := range b.BootSourceOverrideEnabledAllowed {
			if allowed == wanted {
				modes = append(modes, wanted)
			}
		}
	}
	return modes
}

// targetAllowed reports whether the System allows a BootSourceOverrideTarget value. A System
// that does not advertise allowable values is assumed to allow it.
func (d *discovered) targetAllowed(target string) bool {
	if d.system.Boot == nil {
		return false
	}
	allowed := d.system.Boot.BootSourceOverrideTargetAllowed
	if len(allowed) == 0 {
		return true
	}
	for _, value := range allowed {
		if value == target {
			return true
		}
	}
	return false
}

// cds returns the virtual CD devices Boot Media can use: those InsertMedia can mount, and those
// already holding media that offer EjectMedia, because some BMCs list InsertMedia only on an
// empty device. insert picks among them only a free device with an InsertMedia target.
func (d *discovered) cds() []virtualMedia {
	var cds []virtualMedia
	for _, media := range d.media {
		if media.isCD() && (media.canInsert() || (media.inserted() && media.canEject())) {
			cds = append(cds, media)
		}
	}
	return cds
}

// mediaHolding returns the virtual CD that currently holds isoURL, or nil.
func (d *discovered) mediaHolding(isoURL string) *virtualMedia {
	for i := range d.media {
		media := d.media[i]
		if media.isCD() && media.inserted() && d.profile.imageMatches(media.Image, media.ImageName, isoURL) {
			return &d.media[i]
		}
	}
	return nil
}

// state summarises the live Boot Media state for isoURL.
func (d *discovered) state(isoURL string) serverdomain.BootMediaState {
	var state serverdomain.BootMediaState
	if media := d.mediaHolding(isoURL); media != nil {
		state.MediaInserted = true
		state.MediaImage = media.Image
	}
	if b := d.system.Boot; b != nil {
		state.OverrideEnabled = b.BootSourceOverrideEnabled
		state.OverrideTarget = b.BootSourceOverrideTarget
		// overrideTargetsCD decides per strategy; a vendor strategy may direct the boot with the
		// Redfish override Disabled, so the override's own mode must not be required here.
		state.OverrideReady = d.overrideTargetsCD()
	}
	return state
}

// insert mounts isoURL on a free virtual CD. swallow sends no UserName/Password (the Boot ISO is
// served without authentication, and some firmware rejects them for HTTP); the transfer protocol
// follows the URL scheme. A 202 answer is a Redfish Task that is followed to its end.
func (c *Controller) insert(ctx context.Context, s *session, found *discovered, isoURL string) error {
	var target *virtualMedia
	for _, cd := range found.cds() {
		if !cd.inserted() && cd.canInsert() {
			cd := cd
			target = &cd
			break
		}
	}
	if target == nil {
		return &controllerError{sentinel: serverdomain.ErrBootMediaRejected, detail: "Every virtual CD already holds other media; eject one first.", operation: "mount the ISO", permanent: true}
	}
	if found.profile.ejectStaleImage && strings.TrimSpace(target.Image) != "" {
		// A device that dropped its mount keeps naming the image (Inserted false), and the
		// profile's firmware refuses to mount it again until it is ejected, so clear it first.
		if err := c.eject(ctx, s, *target); err != nil {
			return err
		}
		if err := sleep(ctx, c.poll); err != nil {
			return classify(err)
		}
	}
	protocol := "HTTP"
	if strings.HasPrefix(strings.ToLower(isoURL), "https://") {
		protocol = "HTTPS"
	}
	body := map[string]any{"Image": isoURL, "TransferProtocolType": protocol, "Inserted": true, "WriteProtected": true}
	resp, err := s.do(ctx, http.MethodPost, target.Actions.Insert.Target, body, nil)
	// Each retry drops at least one optional parameter, so this ends after a few requests.
	for dropped := refusedInsertParameters(err, body); len(dropped) > 0; dropped = refusedInsertParameters(err, body) {
		for _, name := range dropped {
			delete(body, name)
		}
		resp, err = s.do(ctx, http.MethodPost, target.Actions.Insert.Target, body, nil)
	}
	if err != nil {
		return insertError(err)
	}
	return c.followTask(ctx, s, resp, "mount the ISO")
}

// optionalInsertParameters are the InsertMedia parameters DMTF makes optional, which a BMC may
// not support; Image is required and never dropped.
var optionalInsertParameters = []string{"Inserted", "WriteProtected", "TransferProtocolType"}

// refusedInsertParameters returns the optional parameters of body to drop after InsertMedia
// answered err: the one a 400 names in its DMTF message arguments (PropertyUnknown,
// ActionParameterUnknown, ActionParameterNotSupported, …), or Inserted and WriteProtected for a
// parameter refusal that names none. It returns nil for any other failure, for a refused URL
// format (the URL, not a parameter, is wrong), and when nothing it would drop is left, which ends
// the caller's retries.
func refusedInsertParameters(err error, body map[string]any) []string {
	var status *statusError
	if !errors.As(err, &status) || status.Status != http.StatusBadRequest || strings.Contains(status.MessageID, "ValueFormat") {
		return nil
	}
	for _, arg := range status.MessageArgs {
		for _, name := range optionalInsertParameters {
			if _, present := body[name]; present && strings.EqualFold(arg, name) {
				return []string{name}
			}
		}
	}
	if !strings.Contains(status.MessageID, "ActionParameter") && !strings.Contains(status.MessageID, "Property") {
		return nil
	}
	var dropped []string
	for _, name := range []string{"Inserted", "WriteProtected"} {
		if _, present := body[name]; present {
			dropped = append(dropped, name)
		}
	}
	return dropped
}

// insertError explains a refused InsertMedia, including the format refusal some firmware gives
// an https:// image.
func insertError(err error) error {
	var status *statusError
	if errors.As(err, &status) && strings.Contains(status.MessageID, "ValueFormat") {
		return &controllerError{sentinel: serverdomain.ErrBootMediaRejected,
			detail:    "The BMC rejected the ISO URL's format (" + status.Message + "); many BMCs accept only http:// on port 80.",
			operation: status.operation(), permanent: true}
	}
	return classify(err)
}

// followTask waits for the Task an action answered 202 with. Synchronous answers return at once.
func (c *Controller) followTask(ctx context.Context, s *session, resp *response, what string) error {
	if resp == nil || resp.Status != http.StatusAccepted {
		return nil
	}
	monitor := resp.Location
	if monitor == "" {
		var created struct {
			ID string `json:"@odata.id"`
		}
		if json.Unmarshal(resp.Body, &created) == nil {
			monitor = created.ID
		}
	}
	if monitor == "" {
		return nil
	}
	if parsed, err := url.Parse(monitor); err == nil && parsed.IsAbs() {
		monitor = parsed.RequestURI()
	}
	deadline := time.Now().Add(c.taskWait)
	for time.Now().Before(deadline) {
		if err := sleep(ctx, c.poll); err != nil {
			return classify(err)
		}
		var t task
		if err := s.get(ctx, monitor, &t); err != nil {
			var status *statusError
			if errors.As(err, &status) && status.Status == http.StatusNotFound {
				// Some BMCs drop a finished task immediately; the caller re-reads the result.
				return nil
			}
			return classify(err)
		}
		if !t.terminal() {
			continue
		}
		if t.TaskState == "Completed" && (t.TaskStatus == "" || t.TaskStatus == "OK" || t.TaskStatus == "Warning") {
			return nil
		}
		return &controllerError{sentinel: serverdomain.ErrBootMediaRejected,
			detail:    fmt.Sprintf("The BMC could not %s: %s", what, nonEmpty(t.lastMessage(), t.TaskState)),
			operation: what}
	}
	return &controllerError{sentinel: serverdomain.ErrBootMediaRejected, detail: fmt.Sprintf("The BMC did not finish trying to %s within %s.", what, c.taskWait), operation: what}
}

// mountAndSettle inserts isoURL, waits until the BMC reports it inserted, then waits the profile's
// settle and checks it is still inserted, re-inserting once if the BMC dropped it. It returns a
// fresh discovery. See Controller.mountSettle for why a mount must settle before the host powers
// on.
func (c *Controller) mountAndSettle(ctx context.Context, s *session, endpoint serverdomain.BMCEndpoint, found *discovered, isoURL string) (*discovered, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			serverdomain.ReportBootMediaPhase(ctx, serverdomain.BootMediaPhaseMounting, nil)
		}
		if err := c.insert(ctx, s, found, isoURL); err != nil {
			return nil, err
		}
		if _, err := c.awaitInserted(ctx, s, endpoint, isoURL); err != nil {
			return nil, err
		}
		settle := c.settleFor(found.profile)
		settled := time.Now().UTC().Add(settle).Truncate(time.Millisecond)
		serverdomain.ReportBootMediaPhase(ctx, serverdomain.BootMediaPhaseSettling, &settled)
		if err := sleep(ctx, settle); err != nil {
			return nil, classify(err)
		}
		var err error
		if found, err = s.discover(ctx, endpoint); err != nil {
			return nil, discoveryError(err)
		}
		if found.mediaHolding(isoURL) != nil {
			return found, nil
		}
		// The BMC dropped the fresh mount. A device that still names the image but reports
		// Inserted false counts as free, so the second attempt mounts it again.
	}
	return nil, &controllerError{sentinel: serverdomain.ErrBootMediaRejected, detail: "The BMC mounted the ISO but dropped it again within a minute.", operation: "mount the ISO"}
}

// awaitInserted re-reads until a virtual CD reports isoURL inserted.
func (c *Controller) awaitInserted(ctx context.Context, s *session, endpoint serverdomain.BMCEndpoint, isoURL string) (*discovered, error) {
	deadline := time.Now().Add(c.settleWait)
	for {
		found, err := s.discover(ctx, endpoint)
		if err != nil {
			return nil, discoveryError(err)
		}
		if found.mediaHolding(isoURL) != nil {
			return found, nil
		}
		if time.Now().After(deadline) {
			return nil, &controllerError{sentinel: serverdomain.ErrBootMediaRejected, detail: "The BMC accepted the ISO but never reported it inserted.", operation: "mount the ISO"}
		}
		if err := sleep(ctx, c.poll); err != nil {
			return nil, classify(err)
		}
	}
}

// eject removes the media from one device.
func (c *Controller) eject(ctx context.Context, s *session, media virtualMedia) error {
	if media.Actions.Eject == nil || media.Actions.Eject.Target == "" {
		return &controllerError{sentinel: serverdomain.ErrRedfishUnsupported, detail: "The virtual CD offers no eject action.", operation: "eject the ISO", permanent: true}
	}
	resp, err := s.do(ctx, http.MethodPost, media.Actions.Eject.Target, map[string]any{}, nil)
	if err != nil {
		return classify(err)
	}
	return c.followTask(ctx, s, resp, "eject the ISO")
}

// patchBoot changes System.Boot, sending the System's ETag when it has one (some BMCs require
// If-Match and answer 412 for a stale tag, which is retried once with a fresh read).
func (c *Controller) patchBoot(ctx context.Context, s *session, system computerSystem, changes map[string]any) error {
	etag := system.ETag
	for attempt := 0; attempt < 2; attempt++ {
		header := http.Header{}
		if etag != "" {
			header.Set("If-Match", etag)
		}
		_, err := s.do(ctx, http.MethodPatch, system.ODataID, map[string]any{"Boot": changes}, header)
		if err == nil {
			return nil
		}
		var status *statusError
		if attempt == 0 && errors.As(err, &status) && status.Status == http.StatusPreconditionFailed {
			var fresh computerSystem
			if err := s.get(ctx, system.ODataID, &fresh); err != nil {
				return classify(err)
			}
			etag = fresh.ETag
			continue
		}
		return classify(err)
	}
	return &controllerError{sentinel: serverdomain.ErrBootMediaRejected, detail: "The BMC kept refusing the boot override change.", operation: "PATCH " + system.ODataID}
}

// sleep waits d or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return &unreachableError{cause: ctx.Err()}
	case <-time.After(d):
		return nil
	}
}

func nonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
