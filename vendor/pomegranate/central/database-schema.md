# BoltDB Schema

## `emails`

- Key: `[email][profile-handler-pubkey]`
- Value: `nil`

This bucket works like a secondary index. Value carries no payload.

## `profiles`

- Key: `[profile-handler-pubkey]`
- Value: JSON object with:
  - `handler-secret-key` (corresponding to the handler public key)
  - `name`
  - `restrictions` (Nostr filter)
  - `email` (base email owning this profile)

These keys are profile handler pubkeys registered through profile management API.

## `account`

- Key: `[email]`
- Value: JSON object with:
  - `operators`: array of objects with
    - `url`: string URL of this operator
    - `pubshard`: public key shard associated with this operator
  - `threshold`: number

This is usually configured once and holds the data we need to actually sign an event.
