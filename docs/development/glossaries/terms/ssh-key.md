# SSH Key

- Bounded context: Access.
- Definition: A swallow-owned record of one SSH public key that swallow authorizes on
  the Servers it deploys. Every SSH Key has exactly one purpose: `deployment` (the single
  Deployment Key swallow itself logs in with) or `access` (an Access Key a person uses to
  log in).
- Allowed meaning: A named public key identified by a swallow-issued id and by its SHA256
  fingerprint; unique by key material across the installation. An **Access Key** is owned
  by one swallow User (today always the bootstrap admin) and is public-key only: when
  swallow generates an Access Key pair it returns the private key exactly once and never
  stores it, and an imported Access Key is a public key only. swallow realizes every SSH
  Key's public key into each provisioner Integration that offers key registration, so OS
  Deployments through that provisioner authorize it, and records a per-Integration sync
  state (`synced`, `pending`, `failed`, `unsupported`).
- Disallowed meaning: Not a login credential swallow holds for a person — swallow never
  stores an Access Key's private key. Not the Site Automation Configuration's optional
  private-key override. Not a provider-owned fact: the provisioner's own key list is a
  realization target, and keys an operator added there directly are not SSH Keys and are
  never removed by swallow. Not a guarantee that already-deployed Servers change: a
  provisioner injects keys at OS Deployment time only.
- Synonyms: Access Key (for purpose `access`).
- Deprecated terms: None.
- Examples: "The admin imports their laptop's `ssh-ed25519` public key as an Access Key;
  swallow registers it in the site's MAAS so the next deployed Server accepts it." /
  "Generating an Access Key downloads the private key once; after the dialog closes it
  cannot be retrieved again." / "An Access Key whose provisioner is not key-capable shows
  sync state `unsupported`."
- Related terms: Deployment Key, User, Integration, OS Provisioning Provider,
  OS Deployment, Automation Configuration, Server.
- Change note: Added for SSH key management
  ([decision 039](../../../decisions/039-ssh-key-management-and-default-user.md)),
  promoting the pending term `SSH Key`. Ownership is by User so the model survives the
  planned multi-user work; today only the bootstrap admin exists.
