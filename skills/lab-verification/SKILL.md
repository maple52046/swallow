---
name: lab-verification
description: >-
  Rules for verifying swallow end to end on the development stack and the lab's real machines:
  which machines a drill may use, what to ask first, and how to keep credentials out of logs and
  commits. Use before deploying, uninstalling, or otherwise acting on lab Servers, or before
  running anything against the development stack.
---

# Lab verification

## When to Use This Skill

- Verifying a change end to end on real machines (an OS deployment, a Platform or software
  install or uninstall, a playbook, a provider recovery).
- Running anything against the development stack (`deploy/dev`), including API calls that change
  state.

Automated tests are described in `docs/development/development-and-testing.md`; this skill covers
only what happens outside them.

## Before a drill

1. **Ask first.** Do not run anything against the development stack or the lab without the user's
   go-ahead for that drill.
2. **Name the machines.** Say which Servers the drill will use before starting, and why.
3. **Use only `lab-` machines and `rich-gecko`.** Drills use virtual machines whose names start with
   `lab-`, and the physical Supermicro AS-4124GQ-TNMI `rich-gecko` (formerly `pvt250`; MAAS may
   rename it, so identify it by that model), which the user set aside for hypervisor experiments.
   Prefer a RAM deployment on `rich-gecko`: its disks hold the user's data, and the user must agree
   before a drill erases them. Never act on a machine whose name starts with `tainan-`, and never
   on the hypervisor `tainan-ci` that hosts the lab VMs; they are shared physical machines. With
   the user's go-ahead, `tainan-ci`'s configuration may be read for reference, never changed.
4. **Keep provider data.** Never delete Machines, images, or other data in MAAS; release or
   recover through swallow instead.

## During a drill

- **Hot reload restarts processes.** The development `api-server`, `worker`, and
  `ansible-executor` run under Air: saving a Go file under `api-server/` rebuilds and restarts all
  three, and a running Ansible Task then stops in `requires_attention`
  (`executor_outcome_unknown`). Do not edit api-server Go code while a drill's Task runs; check the
  executor's recent logs first (`docker logs --since 10m swallow-dev-ansible-executor-1`).
- **Keep secrets out of output.** Tokens, passwords, API keys, and private keys must never appear in
  command output you share, in logs, or in commits. Avoid commands that print credentials (for
  example `maas list`, which prints the profile's API key). Keep a session token in a file outside
  the repository and delete it, and any helper script that embeds a credential, when the drill ends.
- **Prefer reading.** Inspect state through the API, logs, and read-only provider calls; change it
  only through swallow, so the change is recorded as a Workflow.

## After a drill

- Return the machines to the state they were in (released or deployed as before), unless the user
  asks to keep the result.
- Report which machines were used, what ran, and the Workflow ids, without credentials.
