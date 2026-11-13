package app

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"github.com/maple52046/swallow/config"
	ldapdirectory "github.com/maple52046/swallow/internal/adapters/directory/ldap"
	authstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/auth"
	sshkeystore "github.com/maple52046/swallow/internal/adapters/store/mongodb/sshkey"
	accessapp "github.com/maple52046/swallow/internal/core/access/application"
	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
	sshkeydomain "github.com/maple52046/swallow/internal/core/sshkey/domain"
	"github.com/maple52046/swallow/internal/core/store"
	"github.com/maple52046/swallow/internal/shared/secret"

	"go.mongodb.org/mongo-driver/mongo"
)

// directoryControllerDeps are what the worker hands newDirectoryController.
type directoryControllerDeps struct {
	cfg        config.DirectoryConfig
	db         *mongo.Database
	sealer     *secret.Sealer
	transactor store.Transactor
	stores     accessStores
	servers    serverdomain.ServerRepository
	pools      infradomain.PoolRepository
	platforms  platformdomain.PlatformRepository
	modules    *moduledomain.Registry
}

// newDirectoryController assembles the Directory controller over the LDAP adapter (decision 082).
// The writer password and the CA are read once here; a missing or unreadable file fails the worker's
// start, since a configured Directory that cannot be written would leave every Server's accounts
// stale without saying so.
func newDirectoryController(deps directoryControllerDeps) (*accessapp.DirectoryController, error) {
	password, err := readSecretFile(deps.cfg.BindPasswordFile)
	if err != nil {
		return nil, fmt.Errorf("directory bind password: %w", err)
	}
	caPEM, err := os.ReadFile(deps.cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("directory CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("directory CA: no certificate in " + deps.cfg.CAFile)
	}
	keys, err := sshkeystore.NewMongoKeyRepo(deps.db, deps.sealer)
	if err != nil {
		return nil, fmt.Errorf("ssh key repo init: %w", err)
	}
	directory, err := ldapdirectory.New(ldapdirectory.Config{
		URL: deps.cfg.URL, BaseDN: deps.cfg.BaseDN, BindPassword: password, RootCAs: roots,
	})
	if err != nil {
		return nil, err
	}
	return accessapp.NewDirectoryController(accessapp.DirectoryControllerDeps{
		Users: authstore.NewMongoUserRepo(deps.db), Memberships: deps.stores.memberships, Policies: deps.stores.policies,
		Settings: deps.stores.settings, Groups: deps.stores.posixIDs, Statuses: deps.stores.statuses,
		Inputs: directoryInputs{
			servers: deps.servers, pools: deps.pools, platforms: deps.platforms, keys: keys, modules: deps.modules,
		},
		Directory: directory, Transactor: deps.transactor,
	})
}

// directoryInputs adapts the stores other contexts own onto accessdomain.DirectoryInputs.
type directoryInputs struct {
	servers   serverdomain.ServerRepository
	pools     infradomain.PoolRepository
	platforms platformdomain.PlatformRepository
	keys      sshkeydomain.KeyRepository
	modules   *moduledomain.Registry
}

var _ accessdomain.DirectoryInputs = directoryInputs{}

// Servers implements accessdomain.DirectoryInputs: every present Server with its Pool, the names it
// answers to, and its addresses. An absent Server is left out, so its host names leave the Directory.
func (in directoryInputs) Servers(ctx context.Context) ([]accessdomain.DirectoryServer, error) {
	result, err := in.servers.List(ctx, serverdomain.ListFilter{})
	if err != nil {
		return nil, err
	}
	out := make([]accessdomain.DirectoryServer, 0, len(result.Servers))
	for _, server := range result.Servers {
		var names []string
		for _, name := range []string{server.Observed.Hostname, server.Observed.FQDN} {
			if name != "" {
				names = append(names, name)
			}
		}
		out = append(out, accessdomain.DirectoryServer{
			ID: server.ID, PoolID: server.Spec.PoolID, Names: names, Addresses: server.Observed.Addresses,
		})
	}
	return out, nil
}

// Pools implements accessdomain.DirectoryInputs: every Pool of the installation.
func (in directoryInputs) Pools(ctx context.Context) ([]accessdomain.DirectoryPool, error) {
	pools, err := in.pools.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]accessdomain.DirectoryPool, 0, len(pools))
	for _, pool := range pools {
		out = append(out, accessdomain.DirectoryPool{ID: pool.ID, Name: pool.Name})
	}
	return out, nil
}

// AccessKeys implements accessdomain.DirectoryInputs: each User's Access Keys as authorized_keys
// lines. The Deployment Key belongs to no User and never reaches the Directory.
func (in directoryInputs) AccessKeys(ctx context.Context) (map[string][]string, error) {
	keys, err := in.keys.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, key := range keys {
		if key.Purpose == sshkeydomain.PurposeAccess && key.OwnerUserID != "" {
			out[key.OwnerUserID] = append(out[key.OwnerUserID], key.PublicKey)
		}
	}
	return out, nil
}

// HostAccessLimits implements accessdomain.DirectoryInputs from the Platform modules' hostAccess
// declarations: each spec member of a Platform whose module declares one gets the rule of its node
// roles. A Platform without a spec, or whose module declares nothing, limits nothing.
func (in directoryInputs) HostAccessLimits(ctx context.Context) (map[string]accessdomain.HostAccessLimit, error) {
	platforms, err := in.platforms.List(ctx, "")
	if err != nil {
		return nil, err
	}
	out := map[string]accessdomain.HostAccessLimit{}
	for _, platform := range platforms {
		if platform.Spec == nil {
			continue
		}
		module, ok := in.modules.Module(moduledomain.KindPlatform, string(platform.Type))
		if !ok || module.Manifest.HostAccess == nil {
			continue
		}
		for _, member := range platform.Spec.Members {
			rule := module.Manifest.HostAccess.RuleFor(member.Roles)
			out[member.ServerID] = accessdomain.HostAccessLimit{MemberLogin: rule.MemberLogin, MemberSudo: rule.MemberSudo}
		}
	}
	return out, nil
}

// directoryClientSettings implements softwareapp.DirectoryClientSettings from the installation's
// Directory configuration (decision 082). The CA and the reader password are read from their files
// on every install, so a rotated secret reaches the next install without a restart; neither is ever
// logged. The Users' names go to the host's conflict check, which refuses a local account that
// would shadow a Directory account.
type directoryClientSettings struct {
	cfg      config.DirectoryConfig
	users    authdomain.UserRepository
	posixIDs interface {
		RecordedStart(ctx context.Context) (int, bool, error)
	}
	// defaultStart is the configured first POSIX id, used until the first allocation records one.
	defaultStart int
}

var _ softwareapp.DirectoryClientSettings = directoryClientSettings{}

// DirectoryClient implements softwareapp.DirectoryClientSettings.
func (s directoryClientSettings) DirectoryClient(ctx context.Context) (map[string]any, map[string]any, bool, error) {
	if !s.cfg.Configured() {
		return nil, nil, false, nil
	}
	caPEM, err := os.ReadFile(s.cfg.CAFile)
	if err != nil {
		return nil, nil, true, fmt.Errorf("read the directory CA: %w", err)
	}
	password, err := readSecretFile(s.cfg.ReaderPasswordFile)
	if err != nil {
		return nil, nil, true, fmt.Errorf("read the directory reader password: %w", err)
	}
	start, recorded, err := s.posixIDs.RecordedStart(ctx)
	if err != nil {
		return nil, nil, true, err
	}
	if !recorded {
		start = s.defaultStart
	}
	users, err := s.users.List(ctx)
	if err != nil {
		return nil, nil, true, err
	}
	usernames := make([]string, 0, len(users))
	for _, user := range users {
		usernames = append(usernames, user.Username)
	}
	trusted := map[string]any{
		"swallow_directory_uri":       s.cfg.HostURL,
		"swallow_directory_base_dn":   s.cfg.BaseDN,
		"swallow_directory_ca_pem":    string(caPEM),
		"swallow_directory_id_start":  start,
		"swallow_directory_usernames": usernames,
	}
	secrets := map[string]any{
		"swallow_directory_bind_dn":       "cn=reader,ou=services," + s.cfg.BaseDN,
		"swallow_directory_bind_password": password,
	}
	return trusted, secrets, true, nil
}

// newDirectoryClientSettings builds the directory client settings of a process; an installation
// without a Directory gets nil, which refuses the directory client's install.
func newDirectoryClientSettings(cfg config.APIConfig, db *mongo.Database, stores accessStores) softwareapp.DirectoryClientSettings {
	if !cfg.Directory.Configured() {
		return nil
	}
	return directoryClientSettings{
		cfg: cfg.Directory, users: authstore.NewMongoUserRepo(db), posixIDs: stores.posixIDs, defaultStart: cfg.POSIXIDStart,
	}
}
