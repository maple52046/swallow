import { useEffect, useState } from "react";
import { Button, Card, Field, HStack, Input } from "@chakra-ui/react";
import { useNavigate } from "react-router-dom";
import { useApp } from "@/di/AppProvider";
import { formatMinimumResources } from "@/domain/platform/resourceEligibility";
import type { MinimumResources } from "@/domain/platform/types";
import { ErrorState } from "@/presentation/components/ErrorState";
import { LoadingState } from "@/presentation/components/LoadingState";
import { PageHeader } from "@/presentation/components/PageHeader";
import { Alert } from "@/presentation/components/ui/alert";
import { Checkbox } from "@/presentation/components/ui/checkbox";
import { useToast } from "@/presentation/components/toast/toastContext";
import { useSiteScope } from "@/presentation/contexts/SiteScopeContext";

/**
 * Global Platform administration for deployment eligibility. Values are entered in operator-
 * friendly GiB but converted to the provider-owned MiB contract only when loading or saving.
 */
export function PlatformSettingsPage() {
  const navigate = useNavigate();
  const { platforms } = useApp();
  const { scopedHref } = useSiteScope();
  const { showToast } = useToast();
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [saving, setSaving] = useState(false);
  const [enabled, setEnabled] = useState(false);
  const [cpuCores, setCpuCores] = useState("4");
  const [memoryGiB, setMemoryGiB] = useState("24");
  const [storageGB, setStorageGB] = useState("80");
  const [current, setCurrent] = useState<MinimumResources | null>(null);

  useEffect(() => {
    let cancelled = false;
    platforms
      .getSlurmDeploymentRequirement()
      .then((requirement) => {
        if (cancelled) return;
        setCurrent(requirement.minimumResources);
        setEnabled(Boolean(requirement.minimumResources));
        if (requirement.minimumResources) {
          setCpuCores(String(requirement.minimumResources.cpuCores));
          setMemoryGiB(String(requirement.minimumResources.memoryMiB / 1024));
          setStorageGB(String(requirement.minimumResources.storageGB));
        }
        setLoadError("");
      })
      .catch((error: Error) => {
        if (!cancelled) setLoadError(error.message);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [platforms]);

  const cpu = Number(cpuCores);
  const memory = Number(memoryGiB);
  const storage = Number(storageGB);
  const valuesValid =
    Number.isInteger(cpu) &&
    cpu > 0 &&
    Number.isInteger(memory) &&
    memory > 0 &&
    Number.isFinite(storage) &&
    storage > 0;
  const canSave = !saving && (!enabled || valuesValid);

  const save = async () => {
    if (!canSave) return;
    setSaving(true);
    const minimum: MinimumResources | null = enabled
      ? { cpuCores: cpu, memoryMiB: memory * 1024, storageGB: storage }
      : null;
    try {
      const requirement =
        await platforms.putSlurmDeploymentRequirement(minimum);
      setCurrent(requirement.minimumResources);
      showToast({
        title: "Slurm deployment requirement updated",
        description: requirement.minimumResources
          ? `Eligible Servers must provide ${formatMinimumResources(requirement.minimumResources)}.`
          : "Resource filtering is disabled.",
        tone: "success",
      });
    } catch (error) {
      showToast({
        title: "Could not update requirement",
        description:
          error instanceof Error ? error.message : "The setting was not saved.",
        tone: "error",
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="operator-page">
      <PageHeader
        title="Platform settings"
        breadcrumbs={[
          { label: "Platforms", href: scopedHref("/platforms") },
          { label: "Settings" },
        ]}
      />
      {loading && <LoadingState rows={4} />}
      {!loading && loadError && <ErrorState message={loadError} />}
      {!loading && !loadError && (
        <Card.Root>
          <Card.Body gap="5">
            <Alert status="info" title="Slurm minimum resource requirement">
              This is a deployment eligibility floor based on observed hardware.
              It does not reserve resources or guarantee that every OS image
              fits an ephemeral root filesystem.
            </Alert>
            <Checkbox
              id="slurm-requirement-enabled"
              checked={enabled}
              onCheckedChange={setEnabled}
            >
              Enforce a minimum for Slurm deployment nodes
            </Checkbox>
            <div className="sw-form-grid">
              <Field.Root
                required={enabled}
                invalid={enabled && (!Number.isInteger(cpu) || cpu <= 0)}
              >
                <Field.Label>CPU cores</Field.Label>
                <Input
                  type="number"
                  min={1}
                  step={1}
                  value={cpuCores}
                  disabled={!enabled}
                  onChange={(event) => setCpuCores(event.target.value)}
                />
                <Field.ErrorText>
                  Enter a positive whole number.
                </Field.ErrorText>
              </Field.Root>
              <Field.Root
                required={enabled}
                invalid={enabled && (!Number.isInteger(memory) || memory <= 0)}
              >
                <Field.Label>Memory GiB</Field.Label>
                <Input
                  type="number"
                  min={1}
                  step={1}
                  value={memoryGiB}
                  disabled={!enabled}
                  onChange={(event) => setMemoryGiB(event.target.value)}
                />
                <Field.ErrorText>
                  Enter a positive whole number of GiB.
                </Field.ErrorText>
              </Field.Root>
              <Field.Root
                required={enabled}
                invalid={enabled && !(Number.isFinite(storage) && storage > 0)}
              >
                <Field.Label>Storage GB</Field.Label>
                <Input
                  type="number"
                  min={0.01}
                  step="any"
                  value={storageGB}
                  disabled={!enabled}
                  onChange={(event) => setStorageGB(event.target.value)}
                />
                <Field.ErrorText>
                  Enter a value greater than zero.
                </Field.ErrorText>
              </Field.Root>
            </div>
            <Alert
              status="info"
              title={`Current policy: ${formatMinimumResources(current)}`}
            />
            <HStack justify="flex-end">
              <Button
                variant="outline"
                onClick={() => navigate(scopedHref("/platforms"))}
              >
                Cancel
              </Button>
              <Button
                colorPalette="brand"
                disabled={!canSave}
                loading={saving}
                onClick={() => void save()}
              >
                Save settings
              </Button>
            </HStack>
          </Card.Body>
        </Card.Root>
      )}
    </div>
  );
}
