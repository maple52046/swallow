import { expect, test } from "playwright/test";
import { installApiFixtures } from "./fixtures";


test.beforeEach(async ({ page }) => {
  await installApiFixtures(page, {
    freePlatformCandidates: true,
    slurmRequirementFails: true,
  });
  await page.addInitScript(() => {
    localStorage.setItem("access_token", "e2e-token");
  });
});

test("Slurm policy read failure blocks node selection but Kubernetes remains available", async ({
  page,
}) => {
  await page.goto("/platforms/deploy/kubernetes?site=site-a");
  const next = page.getByRole("button", { name: "Next" });

  await page.getByLabel("Platform name").fill("unaffected-kubernetes");
  await expect(next).toBeEnabled();

  await page.goto("/platforms/deploy/slurm?site=site-a");

  await page.getByLabel("Platform name").fill("blocked-slurm");
  await expect(
    page.getByText("Slurm deployment requirement is unavailable"),
  ).toBeVisible();
  await expect(page.getByText("Requirement store unavailable")).toBeVisible();
  await expect(next).toBeDisabled();
  await next.click({ force: true });
  await expect(
    page.getByRole("heading", { name: "Platform identity" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Nodes and daemons" }),
  ).toHaveCount(0);
});
