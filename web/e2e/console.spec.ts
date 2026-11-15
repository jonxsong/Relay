import { expect, test } from "@playwright/test";

test("connects the public landing page to the operator console", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: /Run agents like production systems/ })).toBeVisible();
  const consoleLink = page.getByRole("link", { name: /Launch operator console/ });
  await expect(consoleLink).toHaveAttribute("href", "/console");
  await consoleLink.click();
  await expect(page).toHaveURL(/\/console$/);
  await expect(page.getByRole("heading", { name: "Execution overview" })).toBeVisible();
});

test("creates, executes, inspects, and cancels a workflow", async ({ page }) => {
  const workflowName = `browser-${Date.now()}`;
  await page.goto("/console");
  await expect(page.getByRole("heading", { name: "Execution overview" })).toBeVisible();
  const apiKey = process.env.RELAY_E2E_API_KEY;
  if (apiKey) {
    const unlockDialog = page.getByRole("dialog", { name: "Unlock Relay" });
    if (!await unlockDialog.isVisible()) {
      await page.getByRole("button", { name: "Unlock console" }).click();
    }
    await unlockDialog.getByRole("textbox", { name: "Relay API key" }).fill(apiKey);
    await unlockDialog.getByRole("button", { name: "Unlock console" }).click();
    await expect(unlockDialog).toBeHidden();
  }
  await page.getByRole("button", { name: "Refresh" }).click();
  await expect(page.getByText("Live", { exact: true })).toBeVisible({ timeout: 15_000 });

  await page.getByRole("button", { name: "Workflows" }).click();
  await page.getByRole("button", { name: "New workflow" }).click();
  const dialog = page.getByRole("dialog", { name: "Create workflow" });
  await dialog.getByRole("textbox", { name: "Name" }).fill(workflowName);
  await dialog.getByRole("textbox", { name: "Description" }).fill("Playwright acceptance workflow");
  await dialog.getByRole("textbox", { name: "Workflow definition" }).fill(JSON.stringify({
    nodes: [{ id: "respond", type: "llm", inputs: { prompt: { from: "$.input.prompt" } } }],
  }, null, 2));
  await dialog.getByRole("button", { name: "Create workflow" }).click();

  const card = page.locator(".workflow-cards article").filter({ hasText: workflowName });
  await expect(card).toBeVisible();
  await card.getByRole("button", { name: "Inspect" }).click();
  await expect(page.locator(".workflow-preview").getByText("respond", { exact: true })).toBeVisible();
  await card.getByRole("button", { name: "Run" }).click();
  const runDialog = page.getByRole("dialog", { name: "Submit a run" });
  await runDialog.getByRole("textbox", { name: "Run input" }).fill('{"prompt":"browser acceptance"}');
  await runDialog.getByRole("button", { name: "Submit run" }).click();

  await page.getByRole("button", { name: "Overview" }).click();
  await expect(page.getByText("respond", { exact: true })).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText(/Run .* · succeeded/)).toBeVisible({ timeout: 15_000 });
  await page.locator(".react-flow__node").filter({ hasText: "respond" }).click();
  const inspector = page.getByLabel("Node attempt inspector");
  await expect(inspector.getByText("Attempt 1", { exact: true })).toBeVisible();
  await inspector.getByRole("button", { name: "Close attempt inspector" }).click();
  await page.getByRole("button", { name: "Usage" }).click();
  await expect(page.getByText("relay-mock-v1")).toBeVisible();
  await page.getByRole("button", { name: "Activity" }).click();
  await page.getByRole("textbox", { name: "Search activity" }).fill("node.succeeded");
  await expect(page.locator(".activity-timeline").getByText("node.succeeded", { exact: true })).toBeVisible();
  await page.getByRole("textbox", { name: "Search activity" }).fill("");
  await page.getByRole("button", { name: "Settings" }).click();
  await expect(page.getByRole("heading", { name: "Browser session" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Lock this console" })).toBeEnabled();

  await page.getByRole("button", { name: "Workflows" }).click();
  await card.getByRole("button", { name: "Run" }).click();
  await runDialog.getByRole("button", { name: "Submit run" }).click();
  await page.getByRole("button", { name: "Overview" }).click();
  const cancel = page.getByRole("button", { name: "Cancel run" });
  await expect(cancel).toBeVisible({ timeout: 10_000 });
  await cancel.click();
  await expect(page.getByText(/Run .* · cancelled/)).toBeVisible({ timeout: 10_000 });
});
