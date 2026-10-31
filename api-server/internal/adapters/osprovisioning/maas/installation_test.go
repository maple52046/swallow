package maas

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

var _ provisioningdomain.InstallationFailureReader = (*Provider)(nil)

// curtinPartitionFailureLog is an excerpt of lab-afde-mi308-3's install.log (MAAS installation
// result 557): curtin could not create partition 2 on a UEFI layout that had no partition 1.
const curtinPartitionFailureLog = `curtin: Installation started. (23.1.1-1118-g3977ce90-0ubuntu1~ubuntu24.04.1)
start: cmd-install/stage-partitioning/builtin/cmd-block-meta: curtin command block-meta
get_blockdev_sector_size: (log=512, phys=512)
nvme0n1 logical_block_size_bytes: 512
An error occured handling 'nvme0n1-part2': RuntimeError - Cannot find previous partition on disk /dev/nvme0n1
finish: cmd-install/stage-partitioning/builtin/cmd-block-meta: FAIL: configuring partition: nvme0n1-part2
TIMED BLOCK_META: 0.855
finish: cmd-install/stage-partitioning/builtin/cmd-block-meta: FAIL: curtin command block-meta
Traceback (most recent call last):
  File "/curtin/curtin/commands/block_meta.py", line 1024, in partition_handler
    raise RuntimeError(
RuntimeError: Cannot find previous partition on disk /dev/nvme0n1
Cannot find previous partition on disk /dev/nvme0n1
curtin: Installation failed with exception: Unexpected error while running command.
`

func TestInstallationErrorLine(t *testing.T) {
	tests := []struct {
		name string
		log  string
		want string
	}{
		{
			name: "curtin handler error is preferred",
			log:  curtinPartitionFailureLog,
			want: "An error occured handling 'nvme0n1-part2': RuntimeError - Cannot find previous partition on disk /dev/nvme0n1",
		},
		{
			name: "traceback exception without a handler error",
			log:  "start: stage\n  File \"x.py\", line 1\nOSError: [Errno 28] No space left on device\nfinish: stage: FAIL: curtin command extract\n",
			want: "OSError: [Errno 28] No space left on device",
		},
		{
			name: "failed stage as the last resort",
			log:  "start: stage\nfinish: cmd-install/stage-extract/builtin/cmd-extract: FAIL: curtin command extract\n",
			want: "finish: cmd-install/stage-extract/builtin/cmd-extract: FAIL: curtin command extract",
		},
		{
			name: "lines mentioning credentials are dropped",
			log: "An error occured handling 'proxy': RuntimeError - bad password for admin\n" +
				"ValueError: token abc123 rejected\n" +
				"finish: stage: FAIL: curtin command apt-config\n",
			want: "finish: stage: FAIL: curtin command apt-config",
		},
		{
			name: "URL credentials are removed",
			log:  "An error occured handling 'mirror': RuntimeError - cannot reach http://mirror:hunter2@archive.example/ubuntu\n",
			want: "An error occured handling 'mirror': RuntimeError - cannot reach http://archive.example/ubuntu",
		},
		{
			name: "no recognizable error",
			log:  "curtin: Installation started.\nstart: stage\n",
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := installationErrorLine(tc.log); got != tc.want {
				t.Errorf("installationErrorLine() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInstallationErrorLineIsBounded(t *testing.T) {
	got := installationErrorLine("RuntimeError: " + strings.Repeat("x", 1000))
	if len([]rune(got)) != maxInstallationErrorRunes || !strings.HasSuffix(got, "...") {
		t.Fatalf("bounded line has %d runes (%q...), want %d ending in an ellipsis",
			len([]rune(got)), got[:20], maxInstallationErrorRunes)
	}
}

// installationResultsJSON mirrors MAAS's unordered installation script sets for one machine:
// the newest failure (557) is listed first and each set carries a binary log archive besides
// the text install.log.
const installationResultsJSON = `[
  {"id": 557, "status_name": "Failed", "type_name": "Installation", "started": "Mon, 19 Oct 2026 05:28:32 +0000",
   "results": [{"id": 1207, "name": "/tmp/curtin-logs.tar"}, {"id": 1206, "name": "/tmp/install.log"}]},
  {"id": 554, "status_name": "Failed", "type_name": "Installation", "started": "Mon, 19 Oct 2026 02:55:15 +0000",
   "results": [{"id": 1203, "name": "/tmp/curtin-logs.tar"}, {"id": 1201, "name": "/tmp/install.log"}]},
  {"id": 556, "status_name": "Failed", "type_name": "Installation", "started": "Mon, 19 Oct 2026 05:21:14 +0000",
   "results": [{"id": 1205, "name": "/tmp/curtin-logs.tar"}, {"id": 1204, "name": "/tmp/install.log"}]}
]`

func TestLatestInstallationFailureReadsNewestTextLog(t *testing.T) {
	fake := newFakeMAAS(t)
	var listQuery, downloadPath, downloadQuery string
	fake.mux.HandleFunc("GET "+apiPrefix+"/nodes/{id}/results/{$}", func(w http.ResponseWriter, r *http.Request) {
		listQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(installationResultsJSON))
	})
	fake.mux.HandleFunc("GET "+apiPrefix+"/nodes/{id}/results/{set}/{$}", func(w http.ResponseWriter, r *http.Request) {
		downloadPath, downloadQuery = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(curtinPartitionFailureLog))
	})
	provider := newTestProvider(t, fake)

	line, err := provider.LatestInstallationFailure(context.Background(), "mcxnhn", time.Time{})
	if err != nil {
		t.Fatalf("LatestInstallationFailure: %v", err)
	}
	if !strings.Contains(line, "Cannot find previous partition on disk /dev/nvme0n1") {
		t.Errorf("line = %q, want the curtin partition error", line)
	}
	if listQuery != "type=installation" {
		t.Errorf("list query = %q, want type=installation", listQuery)
	}
	if downloadPath != apiPrefix+"/nodes/mcxnhn/results/557/" {
		t.Errorf("download path = %q, want the newest set 557", downloadPath)
	}
	for _, want := range []string{"op=download", "output=combined", "filetype=txt", "filters=1206"} {
		if !strings.Contains(downloadQuery, want) {
			t.Errorf("download query = %q, want %s (install.log only, never the archive)", downloadQuery, want)
		}
	}
}

func TestLatestInstallationFailureIgnoresEarlierDeployments(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.respond("GET "+apiPrefix+"/nodes/{id}/results/{$}", http.StatusOK, installationResultsJSON)
	downloads := 0
	fake.mux.HandleFunc("GET "+apiPrefix+"/nodes/{id}/results/{set}/{$}", func(w http.ResponseWriter, _ *http.Request) {
		downloads++
		_, _ = w.Write([]byte(curtinPartitionFailureLog))
	})
	provider := newTestProvider(t, fake)

	since := time.Date(2026, 10, 19, 6, 0, 0, 0, time.UTC)
	line, err := provider.LatestInstallationFailure(context.Background(), "mcxnhn", since)
	if err != nil {
		t.Fatalf("LatestInstallationFailure: %v", err)
	}
	if line != "" || downloads != 0 {
		t.Errorf("line = %q after %d downloads, want none: every failed installation predates this deployment", line, downloads)
	}
}
