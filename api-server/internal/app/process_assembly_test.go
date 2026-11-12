package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.temporal.io/sdk/client"

	"github.com/maple52046/swallow/config"
	"github.com/maple52046/swallow/internal/adapters/store/mongodb/migration"
	"github.com/maple52046/swallow/internal/adapters/store/mongodb/mongotest"
	workflowstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/workflow"
	"github.com/maple52046/swallow/internal/shared/secret"
)

// assemblyFixture is what every process role is assembled over in these tests: a migrated
// throwaway database, the release's modules, a configuration whose paths point inside the
// repository and the test's temporary directories, and a Temporal client that never connects.
type assemblyFixture struct {
	cfg      config.APIConfig
	modules  composedModules
	client   *mongo.Client
	db       *mongo.Database
	sealer   *secret.Sealer
	temporal client.Client
}

func newAssemblyFixture(t *testing.T) assemblyFixture {
	t.Helper()
	mongoClient, db := mongotest.Database(t, "process_assembly")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migration.EnsureInitialized(ctx, db); err != nil {
		t.Fatalf("EnsureInitialized() error = %v", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate credential key: %v", err)
	}
	cfg := config.DefaultConfig().API
	cfg.CredentialKey = base64.StdEncoding.EncodeToString(key)
	cfg.PlaybookManifest = "../../modules/manifest.json"
	cfg.PlaybookDir = "../../modules"
	cfg.JobRuntimeDir = t.TempDir()
	cfg.JobArtifactDir = t.TempDir()
	cfg.BootMedia = config.BootMediaConfig{Dir: t.TempDir(), IPXEDir: t.TempDir()}
	sealer, err := secret.NewSealer(cfg.CredentialKey)
	if err != nil {
		t.Fatalf("NewSealer() error = %v", err)
	}
	modules, err := composeModules()
	if err != nil {
		t.Fatalf("composeModules() error = %v", err)
	}
	// A lazy client dials only on its first call, and nothing here calls it: the roles register
	// their Temporal workers without starting them.
	temporal, err := client.NewLazyClient(client.Options{HostPort: "127.0.0.1:1"})
	if err != nil {
		t.Fatalf("NewLazyClient() error = %v", err)
	}
	t.Cleanup(temporal.Close)
	return assemblyFixture{cfg: cfg, modules: modules, client: mongoClient, db: db, sealer: sealer, temporal: temporal}
}

// The API role assembles over a fresh store: every dependency is satisfied at construction, and the
// app serves the routes the contract lists (routes_contract_test.go checks their exact set).
func TestAPIProcessAssembles(t *testing.T) {
	f := newAssemblyFixture(t)
	process, err := assembleAPI(context.Background(), f.cfg, f.modules, f.client, f.db, f.sealer, "test")
	if err != nil {
		t.Fatalf("assembleAPI() error = %v", err)
	}
	if process.app == nil || process.relay == nil {
		t.Fatalf("assembleAPI() = %+v, want an app and a Server event relay", process)
	}
	served := map[string]bool{}
	for _, route := range process.app.GetRoutes(true) {
		served[route.Method+" "+route.Path] = true
	}
	for _, want := range []string{"GET /api/v1/workflows/", "GET /api/v1/servers/:id", "GET /readyz"} {
		if !served[want] {
			t.Errorf("assembled API does not serve %s", want)
		}
	}
}

// The worker role assembles in both of its steps: the observation passes over the store alone, then
// the Temporal worker and every controller over a Workflow executor client.
func TestWorkerProcessAssembles(t *testing.T) {
	f := newAssemblyFixture(t)
	assembly, err := assembleWorker(f.cfg, f.modules, f.client, f.db, f.sealer)
	if err != nil {
		t.Fatalf("assembleWorker() error = %v", err)
	}
	if assembly.runtime == nil || len(assembly.passes) == 0 {
		t.Fatalf("assembleWorker() runtime = %v, passes = %d; want a runtime and its passes", assembly.runtime, len(assembly.passes))
	}
	execution, err := assembly.execution(context.Background(), f.temporal)
	if err != nil {
		t.Fatalf("worker execution step error = %v", err)
	}
	if execution.temporalWorker == nil {
		t.Fatal("worker execution step built no Temporal worker")
	}
	names := map[string]bool{}
	for _, controller := range execution.controllers {
		if names[controller.Name()] {
			t.Errorf("controller %q is registered twice", controller.Name())
		}
		names[controller.Name()] = true
	}
	for _, want := range []string{"workflow-dispatch", "platform", "local-repository"} {
		if !names[want] {
			t.Errorf("assembled worker has no %s controller; it has %v", want, names)
		}
	}
}

// The Ansible executor role assembles its Temporal worker of the Ansible task queue.
func TestAnsibleExecutorProcessAssembles(t *testing.T) {
	f := newAssemblyFixture(t)
	events, err := workflowstore.NewMongoAnsibleEventRepo(f.db)
	if err != nil {
		t.Fatalf("NewMongoAnsibleEventRepo() error = %v", err)
	}
	ansibleWorker, err := assembleAnsibleExecutor(f.cfg, f.modules, f.client, f.db, f.sealer, events, f.temporal)
	if err != nil {
		t.Fatalf("assembleAnsibleExecutor() error = %v", err)
	}
	if ansibleWorker == nil {
		t.Fatal("assembleAnsibleExecutor() built no Temporal worker")
	}
}
