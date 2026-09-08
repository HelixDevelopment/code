# Operator request history

Zero-loss intake ledger (§11.4.208 / §11.4.210). Every operator request is
recorded here with its content, accepted timestamp + timezone, the track that
processed it, and the alias/model/effort in use — so no request can be skipped,
ignored, or lost.

| id | received | track | alias/model/effort | request | disposition |
|---|---|---|---|---|---|
| REQ-NOTE-0001 | 2026-09-07T13:58+02:00 | T1/main | claude5 / opus / high | NOTE: Claude Toolkit MUST be able to work in BOTH modes — when the project it works on HAS the constitution submodule incorporated, and when it does NOT. | Recorded as durable architectural constraint; relayed to in-flight agents whose designs assume the submodule is present |
