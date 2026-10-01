package app

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	mongoopts "go.mongodb.org/mongo-driver/mongo/options"

	"github.com/maple52046/swallow/config"
	"github.com/maple52046/swallow/internal/migration"
	provisioninginfra "github.com/maple52046/swallow/internal/provisioning/infra"
	"github.com/maple52046/swallow/internal/shared/secret"
	siteinfra "github.com/maple52046/swallow/internal/site/infra"
)

// DeploymentKeyResult reports the outcome of EnsureDeploymentKey without any secret material.
type DeploymentKeyResult struct {
	// Fingerprint is the Deployment Key's OpenSSH SHA256 public fingerprint.
	Fingerprint string
	// Created reports whether this run generated the key (false: it already existed).
	Created bool
}

// EnsureDeploymentKey is the installation step behind `swallow-api deployment-key ensure`
// (decision 039): it creates the installation's Deployment Key when none exists and is a no-op
// otherwise, so swallowctl install, every upgrade, and the dev/testing seeds can all run it.
//
// It is a one-shot process like `migrate`, deliberately separate from the API process: the API
// starts without a Deployment Key, and only OS and Platform deployment require one. It needs the
// same MongoDB and credential key as the API — the private key is sealed with the credential key,
// so running it against a different key would create a key the API cannot open. The schema must
// already be at the current version (run `migrate` first). The key's public half reaches MAAS on
// the API's next sync pass; nothing is sent to a provisioner here.
func EnsureDeploymentKey(cfg config.APIConfig) (DeploymentKeyResult, error) {
	sealer, err := secret.NewSealer(cfg.CredentialKey)
	if err != nil {
		return DeploymentKeyResult{}, fmt.Errorf("credential key: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	client, err := mongo.Connect(ctx, mongoopts.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return DeploymentKeyResult{}, fmt.Errorf("mongo connect: %w", err)
	}
	defer func() {
		disconnectCtx, cancelDisconnect := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancelDisconnect()
		_ = client.Disconnect(disconnectCtx)
	}()
	if err := client.Ping(ctx, nil); err != nil {
		return DeploymentKeyResult{}, fmt.Errorf("mongo ping: %w", err)
	}
	db := client.Database(cfg.MongoDB)
	if err := migration.Check(ctx, db); err != nil {
		return DeploymentKeyResult{}, fmt.Errorf("database schema (run `swallow-api migrate` first): %w", err)
	}

	integrations, err := siteinfra.NewMongoIntegrationRepo(db, sealer)
	if err != nil {
		return DeploymentKeyResult{}, fmt.Errorf("integration repo init: %w", err)
	}
	keys, err := newSSHKeyService(db, sealer, integrations, provisioninginfra.NewProviderFactory(integrations))
	if err != nil {
		return DeploymentKeyResult{}, err
	}
	fingerprint, created, err := keys.EnsureDeploymentKey(ctx)
	if err != nil {
		return DeploymentKeyResult{}, fmt.Errorf("ensure deployment key: %w", err)
	}
	return DeploymentKeyResult{Fingerprint: fingerprint, Created: created}, nil
}
