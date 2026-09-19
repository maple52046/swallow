# Rescue Mode

- Bounded context: OS Provisioning.
- Definition: A provider-backed diagnostic environment in which a Machine boots an ephemeral in-memory OS so an operator can inspect or repair it over SSH, without changing what is installed on disk. It is the normalized `provisioning.state` value `rescue`.
- Allowed meaning: A maintenance state entered on demand and left on demand. Entering is allowed from `deployed`, `broken`, and `failed`. Exiting Rescue Mode restores the Machine to the provider state it had before entering (commonly `deployed`, `broken`, or `failed`); it does not by itself make the Machine `ready`. Reaching `ready` is the job of Recover or Release, not of Rescue Mode. While a Machine is in Rescue Mode it cannot enter normal lifecycle transitions until it is taken out of rescue.
- Disallowed meaning: Not a recovery path to `ready`, and not a way to "fix" a failed deployment. Exiting rescue is not Mark fixed, Recover, or Release. Rescue Mode is not a health axis or a power state; it is a provider lifecycle state on the `provisioning` axis only.
- Synonyms: None. The transient provider labels for entering and exiting rescue — including their failed variants (a provider that reports "failed to exit rescue mode" is still inside the rescue subsystem) — are display-only and normalize to `rescue`, so recovery treats them as a rescue problem (exit rescue, then Release) rather than as a bare failure.
- Deprecated terms: None.
- Examples: "Enter Rescue Mode on a `failed` Server to SSH in and read the installer logs; exiting returns it to `failed`, so use Recover to make it `ready` again." / "Rescue Mode boots Ubuntu in memory; nothing written during rescue survives a normal reboot."
- Related terms: Provider Recovery, Release, Server Status, OS Provisioning Provider, Machine.
- Change note: Added 2026-09-20 with decision 033 to state that Rescue Mode is a diagnostic environment whose exit restores the prior state, separating it from the Recover and Release paths that converge a Server to `ready`.
