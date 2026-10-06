# Ubuntu MAAS 3.6

[繁體中文](README.zh-TW.md) · [Third-party overview](../README.md)

The production installation co-locates MAAS with swallow on one Ubuntu 24.04 VM.
`swallowctl install` installs and owns it through
[`local-maas.sh`](../../production/local-maas.sh); do not install MAAS by hand first,
because the installer refuses a MAAS snap it did not create.

What the installer does:

- installs the MAAS `3.6/stable` snap and initializes it as `region+rack`;
- stores MAAS state in the Compose PostgreSQL instance under its own `maas` role and
  `maasdb` database (loopback only; `maas-test-db` is never used);
- creates the MAAS `admin` account (`secrets/maas-admin-password`) and stores its API key in
  `secrets/maas-api-key`;
- selects the official `images.maas.io` image `ubuntu/noble` amd64 and waits until the boot
  resource is complete;
- registers this MAAS as the Site's provisioner Integration at
  `http://host.docker.internal:5240/MAAS`.

Machines reach MAAS at `http://<primary IPv4>:5240/MAAS`; set `SWALLOW_MAAS_URL` before the
first install to choose another address. Third-party or custom OS images are not installed.

DNS, L2/DHCP/PXE routing, and BMC access remain Site-owned inputs: enable DHCP on the PXE
network in MAAS before network-booting machines. swallow commissions network-booted machines
itself (hardware inspection) once MAAS has enlisted them.

Official references:

- <https://maas.io/docs/release-notes>
- <https://maas.io/docs/how-to-install-maas>
