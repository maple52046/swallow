package infra

import (
	"reflect"
	"testing"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

func TestProvisioningTaskDocumentRoundTripPreservesRecoveryState(t *testing.T) {
	now := time.Date(2026, time.September, 2, 1, 2, 3, 0, time.UTC)
	task := &provisioningdomain.ProvisioningTask{
		ID:                "task-a",
		Kind:              provisioningdomain.ProvisioningTaskReleaseNetworkCleanup,
		ServerID:          "server-a",
		IntegrationID:     "integration-a",
		ProviderMachineID: "machine-a",
		Status:            provisioningdomain.ProvisioningTaskRunning,
		Phase:             provisioningdomain.ProvisioningTaskCleaningNetwork,
		Attempt:           2,
		Snapshot: []provisioningdomain.StaticNetworkLinkSnapshot{{
			InterfaceID: "42",
			LinkID:      "91",
			SubnetID:    "7",
			IPAddress:   "192.0.2.30",
		}},
		Error:      "provider unavailable",
		RequestID:  "request-a",
		NextRunAt:  now.Add(time.Minute),
		LeaseOwner: "worker-a",
		LeaseUntil: now.Add(30 * time.Second),
		CreatedAt:  now,
		UpdatedAt:  now.Add(time.Second),
	}

	restored := provisioningTaskFromDoc(pointerTo(provisioningTaskToDoc(task)))
	if !reflect.DeepEqual(restored, task) {
		t.Fatalf("task document round trip\n got: %#v\nwant: %#v", restored, task)
	}
}

func pointerTo[T any](value T) *T {
	return &value
}
