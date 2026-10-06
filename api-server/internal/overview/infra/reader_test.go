package infra

import (
	"context"
	"testing"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

type overviewServerRepository struct {
	serverdomain.ServerRepository
	result serverdomain.ListResult
}

func (r overviewServerRepository) List(_ context.Context, _ serverdomain.ListFilter) (serverdomain.ListResult, error) {
	return r.result, nil
}

// Overview capacity must not count a BMC graphics controller as workload acceleration.
func TestReaderListServersCountsOnlyComputeGPUs(t *testing.T) {
	repository := overviewServerRepository{result: serverdomain.ListResult{Servers: []*serverdomain.Server{
		{Observed: serverdomain.Observed{GPUs: []serverdomain.GPU{
			{Vendor: "AMD", Model: "MI300X", Count: 8, Kind: serverdomain.GPUKindCompute},
			{Vendor: "ASPEED", Model: "Graphics Family", Count: 1, Kind: serverdomain.GPUKindDisplay},
		}}},
	}}}
	reader := &Reader{servers: repository}

	servers, err := reader.ListServers(context.Background(), "site-a")
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("ListServers length = %d, want 1", len(servers))
	}
	if servers[0].GPUDevices != 8 {
		t.Errorf("GPUDevices = %d, want 8 compute GPUs", servers[0].GPUDevices)
	}
}
