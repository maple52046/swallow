# API keys

[繁體中文](../../zh-TW/guides/api-keys.md) · [Documentation home](../README.md)

An API key lets the CLI, a script, or CI call swallow as you without storing
your password. It acts with your current role until it expires or you delete
it. Use a separate, named key for each client so you can revoke one without
interrupting the others.

API keys authenticate to the swallow API. They are not the SSH access keys used
to log in to Servers and are not the installation's machine token.

## Create a key in the Dashboard

1. Sign in with your password. An existing API key cannot create another key.
2. Open **Account menu → API keys**.
3. Choose **Create API key**, enter a unique name, and choose 30 days, 90 days,
   one year, or no expiry.
4. Copy the `swk_…` secret into a password manager or CI secret store, then
   confirm that you saved it.

The secret is shown only in the creation dialog. swallow keeps only a one-way
hash and cannot recover the secret later. If it is lost, create a replacement
and delete the old key.

The list shows each key's identifying prefix, creation and expiry dates, and
last use. The prefix cannot authenticate. Expired keys remain in the list until
you delete them.

## Use a key with the CLI

Store the key in the CLI profile:

```bash
swallow --endpoint https://swallow.example login --api-key-stdin < ./ci.key
swallow auth me
```

For CI or a single invocation, keep the key in the environment instead:

```bash
SWALLOW_ENDPOINT=https://swallow.example \
  SWALLOW_API_KEY="$CI_SWALLOW_API_KEY" \
  swallow servers list
```

The environment variable overrides the stored profile. Avoid putting the secret
directly in a command line or repository file, where shell history, process
listings, or version control may expose it.

You can also create and manage keys from a password-authenticated CLI session:

```bash
swallow api-keys list
swallow api-keys create --name ci --expires-in 90d --secret-out ./ci.key
swallow api-keys delete <keyId>
```

`--secret-out` creates a new mode-0600 file and never overwrites an existing
file. Without it, the new secret is printed once.

## Expire and revoke keys

An expiry automatically stops a key from authenticating after the chosen time.
Delete a key to revoke it immediately—for example when a runner is retired or a
secret may have leaked. Deleting a key does not end password sessions or affect
other keys.

Keys use their owner's current role rather than carrying independent scopes.
Removing the owner also stops their keys from authenticating.

## Troubleshooting

- **Create is forbidden**: sign out of the API-key profile and sign in with
  your password; key creation requires a Session.
- **A call returns `401 unauthorized`**: the key may be expired, deleted, typed
  incorrectly, or owned by a removed account.
- **The secret is no longer visible**: it cannot be recovered. Create a new key
  and delete the old record.
- **A key is hard to identify**: compare its name and displayed prefix, never
  paste the whole secret into a ticket or log.

Exact limits and authentication behavior are defined by the
[API Keys contract](../../../api-server/docs/development/api-contracts/api-server/api-keys.md).
