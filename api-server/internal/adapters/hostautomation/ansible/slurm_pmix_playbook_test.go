package ansible

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeMPI4Py is a single-rank stand-in for mpi4py: rank FAKE_MPI_RANK of FAKE_MPI_SIZE, whose
// allreduce returns the world size (every rank passed the barrier) unless FAKE_MPI_LOST is set.
const fakeMPI4Py = `import os
class _World:
    def Barrier(self): pass
    def allreduce(self, value, op=None):
        return int(os.environ.get("FAKE_MPI_SIZE", "1")) - (1 if os.environ.get("FAKE_MPI_LOST") else 0)
    def Get_rank(self): return int(os.environ.get("FAKE_MPI_RANK", "0"))
    def Get_size(self): return int(os.environ.get("FAKE_MPI_SIZE", "1"))
class MPI:
    COMM_WORLD = _World()
    SUM = "sum"
`

type mpiProbeReport struct {
	Node          string `json:"node"`
	PMIxNamespace string `json:"pmixNamespace"`
	PMIxRank      string `json:"pmixRank"`
	Rank          int    `json:"rank"`
	Size          int    `json:"size"`
	Barrier       bool   `json:"barrier"`
}

func runMPIProbe(t *testing.T, env []string, args ...string) (mpiProbeReport, int) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not available")
	}
	site := t.TempDir()
	if err := os.MkdirAll(filepath.Join(site, "mpi4py"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "mpi4py", "__init__.py"), []byte(fakeMPI4Py), 0o644); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join("..", "..", "..", "..", "automation", "playbooks", "roles", "slurm_pmix", "files", "mpi-probe.py")
	cmd := exec.Command("python3", append([]string{probe}, args...)...)
	cmd.Env = append(os.Environ(), append(env, "PYTHONPATH="+site)...)
	out, err := cmd.Output()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("mpi-probe: %v", err)
	}
	var report mpiProbeReport
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &report); err != nil {
		t.Fatalf("parse %q: %v", out, err)
	}
	return report, code
}

// Each rank reports its Slurm node (not a container's hostname), its PMIx namespace and rank, and
// whether every rank passed the barrier; --fail-rank makes that rank fail after the barrier.
func TestMPIProbeReportsPMIxWireUp(t *testing.T) {
	env := []string{"FAKE_MPI_SIZE=2", "FAKE_MPI_RANK=1", "SLURMD_NODENAME=node-b",
		"PMIX_NAMESPACE=slurm.pmix.7.0", "PMIX_RANK=1"}
	report, code := runMPIProbe(t, env)
	if code != 0 || !report.Barrier || report.Rank != 1 || report.Size != 2 || report.Node != "node-b" ||
		report.PMIxNamespace != "slurm.pmix.7.0" || report.PMIxRank != "1" {
		t.Errorf("report = %+v, exit %d", report, code)
	}
	if report, _ := runMPIProbe(t, append(env, "FAKE_MPI_LOST=1")); report.Barrier {
		t.Error("a rank that did not reach the allreduce must fail the barrier")
	}
	if _, code := runMPIProbe(t, env, "--fail-rank=1"); code != 3 {
		t.Errorf("--fail-rank=1 on rank 1 exited %d, want 3", code)
	}
	if _, code := runMPIProbe(t, env, "--fail-rank=0"); code != 0 {
		t.Errorf("--fail-rank=0 on rank 1 exited %d, want 0", code)
	}
}

// The deployment proves PMIx with real launches and a failing rank, never with the plugin list alone,
// and keeps MpiDefault none; uninstall removes the probe.
func TestSlurmPMIxIsProvenByLaunches(t *testing.T) {
	verify := slurmPlaybookFile(t, "roles", "slurm_verify", "tasks", "main.yml")
	for _, want := range []string{"--mpi=pmix_v5", "--fail-rank=1", "[slurm_pmix_failed]", "selectattr('barrier')",
		"'inside a container'", "pmixNamespace"} {
		if !strings.Contains(verify, want) {
			t.Errorf("slurm_verify does not contain %q", want)
		}
	}
	if conf := slurmPlaybookFile(t, "roles", "slurm_config", "templates", "slurm.conf.j2"); !strings.Contains(conf, "MpiDefault=none") {
		t.Error("slurm.conf must keep MpiDefault=none")
	}
	if !strings.Contains(slurmPlaybookFile(t, "uninstall-slurm.yml"), "/usr/local/libexec/swallow/mpi-probe") {
		t.Error("uninstall-slurm.yml does not remove the MPI probe")
	}
}
