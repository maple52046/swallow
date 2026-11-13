# SSH Key

- Bounded context: Access.
- Definition: A swallow-owned record of one SSH public key that swallow authorizes on
  the Servers it deploys. Every SSH Key has exactly one purpose: `deployment` (the single
  Deployment Key swallow itself logs in with) or `access` (an Access Key a person uses to
  log in).
- Allowed meaning: A named public key identified by a swallow-issued id and by its SHA256
  fingerprint; unique by key material across the installation. An **Access Key** is owned
  by one swallow User (any User manages their own) and is public-key only: when
  swallow generates an Access Key pair it returns the private key exactly once and never
  stores it, and an imported Access Key is a public key only. swallow realizes the
  Deployment Key and the Access Keys of admins into each provisioner Integration that offers
  key registration, so OS Deployments through that provisioner authorize them on the
  automation account (an admin's way in when the Directory is unreachable); every Access Key
  is also projected into the Directory as its owner's login key, which is how other Users log
  in. An SSH Key is desired state (decision
  071): its spec is the key, the SSH Key Controller realizes it, and its status carries
  per-Integration conditions (`realized`, with reasons such as `capability_missing` or
  `failed`, or `not_registered` for an Access Key whose owner is not an admin); a key removed
  in the provisioner is added back at the controller's resync.
- Disallowed meaning: Not a login credential swallow holds for a person — swallow never
  stores an Access Key's private key. Not a per-Site automation key: Sites have none and
  automation always uses the Deployment Key. Not a provider-owned fact: the provisioner's own key list is a
  realization target, and keys a user added there directly are not SSH Keys and are
  never removed by swallow. Not a guarantee that already-deployed Servers change: a
  provisioner injects keys at OS Deployment time only.
- Synonyms: Access Key (for purpose `access`).
- Deprecated terms: None.
- Examples: "The admin imports their laptop's `ssh-ed25519` public key as an Access Key;
  swallow registers it in the site's MAAS so the next deployed Server accepts it." /
  "Generating an Access Key downloads the private key once; after the dialog closes it
  cannot be retrieved again." / "An Access Key whose provisioner is not key-capable shows
  `realized` false with reason `capability_missing`."
- Related terms: Deployment Key, User, Directory, Integration, OS Provisioning Provider,
  OS Deployment, Automation Configuration, Server, Controller, Condition.
- Change note: Added for SSH key management
  ([decision 039](../../../decisions/039-ssh-key-management-and-default-user.md) (ssh key management and default user)),
  promoting the pending term `SSH Key`. Ownership is by User so the model survives the
  planned multi-user work; today only the bootstrap admin exists. On 2026-10-31
  ([decision 071](../../../decisions/071-provisioner-realized-desired-state.md) (provisioner realized desired state)) the
  per-Integration sync state became conditions written by the SSH Key Controller in the
  worker, replacing the API process's sync loop. On 2026-11-13
  ([decision 082](../../../decisions/082-accounts-pool-access-and-directory.md) (accounts, pool
  access, and directory)) every User manages their own Access Keys, only admins' Access Keys reach
  the provisioners, and every Access Key is projected into the Directory.
