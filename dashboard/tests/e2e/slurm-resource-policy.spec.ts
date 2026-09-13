import { expect, test } from "playwright/test";
import { installApiFixtures } from "./fixtures";

async function chooseSingleSelectOption(
  page: import("playwright/test").Page,
  fieldLabel: string,
  optionLabel: string,
) {
  await page.getByRole("combobox", { name: fieldLabel, exact: true }).click();
  await page.getByRole("option", { name: optionLabel, exact: true }).click();
}

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
  await page.goto("/platforms/deploy?site=site-a");
  const next = page.getByRole("button", { name: "Next" });

  await page.getByLabel("Platform name").fill("unaffected-kubernetes");
  await expect(next).toBeEnabled();

  await chooseSingleSelectOption(page, "Platform type", "Slurm");
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
