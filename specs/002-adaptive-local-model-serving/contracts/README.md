# Interface Contracts

Three interfaces this feature exposes. Each is a contract in the sense that a change to it breaks
someone: users' tool configurations, a consuming tool's parser, or an operator's expectations.

| Contract | Consumer | Breaking-change cost |
|---|---|---|
| [`model-listing.md`](./model-listing.md) | Every consumer, via the existing model-list surface | Users' tool configs carry these names (FR-015) |
| [`selection.md`](./selection.md) | HelixAgent, upper layers, operators | Drives what users are shown and why |
| [`consumer-export.md`](./consumer-export.md) | Claude Toolkit, HelixCode, OpenCode | Each has its own identifier rules |
