# Registry Credential

- Bounded context: Software Deployment (Docker Host Explorer).
- Definition: A swallow-owned, installation-wide username and password (or token) for one container image registry, used when the Docker Host Explorer pulls an image from that registry. It is keyed by the registry's normalized host (with optional port), with at most one credential per registry, and its password is sealed and write-only.
- Allowed meaning: The credential swallow attaches to a single pull when the image reference's registry matches — Docker's rule: the first path component when it contains `.` or `:` or is `localhost`, otherwise Docker Hub (`docker.io`, also written `index.docker.io` or `registry-1.docker.io`; a credential entered as `hub.docker.com`, `registry.hub.docker.com`, or `hub.docker.io` is stored as `docker.io`). A pull without a matching credential is anonymous. Creating, replacing, and deleting a credential never touches a host.
- Disallowed meaning: Not the host's `docker login` state (the Docker Engine API ignores it), not a Docker Engine object, not an Integration (swallow does not reconcile or observe registries), and not scoped to a Site, a Server, or a User. Not readable: no API returns the password.
- Synonyms: Docker registry credential.
- Deprecated terms: None.
- Examples: "Pulling `harbor.lab.local/team/app:1.4` signs in with the `harbor.lab.local` Registry Credential." / "`nginx:1.27` resolves to `docker.io`; without a Docker Hub credential the pull is anonymous." / "Replacing a credential changes its username and password; its registry is fixed."
- Related terms: Docker Host Explorer, Managed Software, Server.
- Change note: Added 2026-10-02 ([decision 044](../../../decisions/044-docker-registry-credentials.md)) so the Docker Host Explorer can pull private images; stored rather than typed per pull by user decision.
