package infra

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	"github.com/maple52046/swallow/internal/shared/secret"
)

func TestNewDeploymentTemplateDocSealsUserData(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	sealer, err := secret.NewSealer(key)
	if err != nil {
		t.Fatalf("new sealer: %v", err)
	}
	plaintext := "#cloud-config\npassword: never-store-this"
	now := time.Now().UTC()
	doc, err := newDeploymentTemplateDoc(&provisioningdomain.DeploymentTemplate{
		ID:            "template-1",
		IntegrationID: "integration-1",
		Name:          "Compute Baseline",
		ImageID:       "ubuntu/jammy",
		CreatedAt:     now,
		UpdatedAt:     now,
	}, plaintext, sealer)
	if err != nil {
		t.Fatalf("build document: %v", err)
	}
	if doc.SealedUserData == "" || doc.SealedUserData == plaintext {
		t.Fatal("Mongo document must contain ciphertext rather than plaintext")
	}
	raw, err := bson.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal BSON: %v", err)
	}
	if strings.Contains(string(raw), plaintext) || strings.Contains(string(raw), "never-store-this") {
		t.Fatal("serialized Mongo document contains plaintext cloud-init")
	}
	opened, err := sealer.Open(doc.SealedUserData)
	if err != nil || opened != plaintext {
		t.Fatalf("sealed user data does not round trip: value=%q err=%v", opened, err)
	}
	if template := toDeploymentTemplate(&doc); !template.HasUserData {
		t.Fatal("read model must report secret presence")
	}
}
