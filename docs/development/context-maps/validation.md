# Validation context map

The `validation` context owns Validation Suites (the catalog of module-declared suites and the
user-defined ones), Validation Runs and their results, and Validation Schedules
([decision 081](../../decisions/081-validation-suites-and-runs.md) (validation suites and runs)). It runs
nothing itself: every run is executed by a Workflow, and its results come back through completion
behaviors the `workflow` context calls.

```text
module registry (validations, templates) ──composition root──> validation suite catalog
server projection (deployed, Site, claim) ──ports──> validation acceptance
platform spec (members, type) + Platform module AccessServers ──ports──> validation acceptance
validation launcher (internal/app) ──template compiler + PrepareAnsibleStep──> workflow plan ──WorkflowService.Create──> workflow store
workflow engine ──validation-result (result) / validation-run-state (projection) completions──> validation run store
workflow internal Runner ──complete-validation──> validation gate assessment (read only)
platform deployment launcher (internal/app) ──composed Jobs + run record, one transaction──> workflow + validation stores
validation-schedule controller (worker) ──change feed: schedules, platforms, workflows──> validation launcher
```

- **Upstream of validation**: the module registry (suite and template declarations), the `server` and
  `platform` contexts (targets, Platform Claims, members, access Servers through the Platform module
  hook), and the `workflow` context (acceptance, ownership, preemption, execution). `validation` reads
  them through application ports implemented in `internal/app`; it never imports another context's
  adapter.
- **Downstream of validation**: none in the core. Deployments compose a suite's Jobs through the
  composition root, which writes the run record in the deployment's own acceptance transaction.
- **Translation**: the module declaration (`validations` in the Module Manifest) is mapped by the
  composition root to the validation domain's Suite; the Workflow Template is compiled by the
  `workflow` domain into a plan; playbook results arrive as the result file of an Ansible Task and are
  mapped by the `validation-result` completion handler, which refuses undeclared checks and targets.
- **Published language**: the validation API contract
  (`api-server/docs/development/api-contracts/api-server/validation.md`), consumed by the dashboard and
  the CLI as conformists.
- **Change impact**: a change to the template format or the completion types affects every module
  that declares a suite; a change to the result shape affects the shared `validation_report` role and
  every validation playbook.
