# API Contracts

API contracts are owned by the component that provides the API, and live in that
component's directory. Today `api-server` is the only provider: its contract
workflow is [`api-server/docs/development/api-contracts/README.md`](../../api-server/docs/development/api-contracts/README.md).
`dashboard` and `cli` consume it and own no contracts.

## Ownership Rule

- A contract belongs to the provider component, not to a consumer.
- A consumer integrates according to the contract and never relies on the
  provider's private implementation; the provider implements the contract.
- If a component starts providing its own API, its contracts live under that
  component's own documentation, and it is added here.
