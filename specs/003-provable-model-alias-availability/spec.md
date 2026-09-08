# Feature Specification: Provable Model Alias Availability

**Feature Branch**: `003-provable-model-alias-availability`
**Created**: 2026-09-08
**Status**: Draft
**Input**: User description: "Every HelixAgent/HelixLLM model must be genuinely usable as a Claude Toolkit provider alias — across native Claude Code, Kimi Code, and all provider aliases — with the capability proven deterministically rather than merely advertised."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Everything listed is usable (Priority: P1)

An operator asks the toolkit what models they can work with. Every entry in the answer is one they can actually select and use. Nothing is listed that will refuse when invoked.

**Why this priority**: This is the whole point of the feature and the largest measured gap. Today the list is not trustworthy — of 27 Kimi twin aliases on the reference host, 8 are usable and 19 present as available and refuse on use. An operator cannot tell which is which without trying each one, so the list actively costs time instead of saving it.

**Independent Test**: Enumerate every alias the toolkit presents, attempt a minimal round trip through each, and compare the two sets. Delivers value on its own: even with nothing else in this feature built, the operator gains a list they can trust.

**Acceptance Scenarios**:

1. **Given** the toolkit presents an alias as available, **When** the operator invokes it, **Then** it completes a round trip and returns model output.
2. **Given** an alias cannot currently be used, **When** the operator lists aliases, **Then** it is either absent from the list or shown with its unusable state and the reason.
3. **Given** an alias is advertised for a model reachable over a particular protocol, **When** it is invoked, **Then** it reaches an endpoint that answers, not one that reports the route does not exist.

---

### User Story 2 - A verdict that means the same thing twice (Priority: P1)

An operator asks whether a given model is working. They get an answer that does not change between runs, and that tells them how old its evidence is.

**Why this priority**: Equal-first with US1 because US1 cannot be verified without it. A verdict that varies run to run cannot establish that anything was fixed, and a verdict with no age reads as present-tense success forever — one alias reported success while a live check of that same endpoint was refusing.

**Independent Test**: Run the same validation repeatedly against unchanged state and compare verdicts byte for byte; separately, confirm each verdict states how old its evidence is. Valuable alone: it makes every other claim in this feature checkable.

**Acceptance Scenarios**:

1. **Given** unchanged system state, **When** validation runs repeatedly, **Then** every run produces an identical verdict.
2. **Given** a verdict was recorded some time ago, **When** the operator views it, **Then** its age is shown and a verdict past the freshness horizon is visibly marked as no longer current.
3. **Given** the age of a verdict cannot be determined, **When** it is displayed, **Then** it is shown as unknown and is NOT presented as recent.
4. **Given** a model whose replies vary between identical requests, **When** validation runs, **Then** the verdict still does not vary, and any check that samples the live model is separately marked as an occasional observation rather than the verdict.

---

### User Story 3 - Honest behaviour where the governance corpus is absent (Priority: P2)

An operator uses the toolkit in a project that has not adopted the shared governance corpus. Everything that can work does; anything that genuinely cannot says so plainly and names what is missing.

**Why this priority**: The toolkit is meant to be portable across projects. Silent no-ops in a project missing the corpus are worse than an error, because the operator believes a protection is active when it is not.

**Independent Test**: Run each entry point from a directory with no governance corpus present and confirm each either works or refuses with a message naming what is unavailable and how to supply it.

**Acceptance Scenarios**:

1. **Given** a project without the governance corpus, **When** the operator runs a toolkit command, **Then** it either completes normally or refuses with a message naming what is unavailable, what still works, and how to fix it.
2. **Given** a command cannot locate the capability it manages, **When** it exits, **Then** its exit status distinguishes "the engine is unreachable" from "the operation ran and found nothing" from "the operation failed".
3. **Given** a command reports finding nothing, **When** the operator inspects why, **Then** they can tell whether it genuinely found nothing or could not look.

---

### User Story 4 - Only what is needed is loaded (Priority: P2)

An operator starts a session and only the capabilities relevant right now are active. Everything else is discoverable and restorable in one command.

**Why this priority**: Directly reduces the cost of every interaction. Deferred behind US1/US2 because a smaller surface is worth little if what remains is not trustworthy.

**Independent Test**: Measure the active capability count and the governance volume carried into a fresh session, then confirm any inactive capability can be listed and restored in a single command.

**Acceptance Scenarios**:

1. **Given** a fresh session, **When** it starts, **Then** only capabilities needed for the current work are active, and the rest are listed as available-but-inactive.
2. **Given** an inactive capability, **When** the operator activates it, **Then** it becomes usable in the running session without restarting.
3. **Given** the operator wants to know what exists, **When** they ask for the catalogue, **Then** they see both active and inactive entries with their state.
4. **Given** a capability was deactivated, **When** the operator looks for how to restore it, **Then** the restore path is reachable without knowing an installation-specific location.

---

### User Story 5 - Navigation that reflects the real codebase (Priority: P3)

An operator or agent searching the codebase gets results covering code the team owns, and is not buried in vendored third-party code.

**Why this priority**: A quality-of-work improvement rather than a correctness one. Ranked last because a wrong navigation result costs time, whereas a wrong availability claim costs trust.

**Independent Test**: Query for a symbol that exists only inside an owned submodule and confirm it resolves; query for vendored third-party code and confirm it is excluded.

**Acceptance Scenarios**:

1. **Given** a symbol defined only within an owned submodule, **When** it is searched for, **Then** it is found.
2. **Given** vendored third-party code in the tree, **When** the index is built, **Then** that code is excluded.
3. **Given** the index is empty or stale, **When** a search runs against it, **Then** the emptiness is reported rather than returned as "no matches".

### Edge Cases

- An alias exists but its supporting configuration does not — the operator must never see it presented as usable. This is the current majority state (19 of 27).
- A capability is deliberately opted out of. The opt-out must survive a session restart rather than being silently undone on next start.
- A model answers correctly but in an unexpected style. Whether that counts as working is a judgement the validation must apply consistently, not per-run.
- A route aggregates several internal calls, so its reported consumption legitimately varies. Validation must not treat that variance as failure.
- The evidence store is reachable but empty. Reporting "nothing found" must be distinguishable from "could not look".
- Two sessions validate concurrently. Neither may record a verdict derived from the other's activity.
- A measuring instrument silently fails and returns nothing. Its empty result must not be read as a clean result — this occurred three times during the investigation that produced this specification.

#### Brainstorm Prompts

- **Boundary conditions**: What is the smallest possible round trip that still proves usability? What is the largest input a route accepts before it degrades, and does it say so or truncate silently?
- **Error scenarios**: What if the model host is down, reachable-but-refusing, or reachable-but-empty? Are those three distinguishable to the operator?
- **Scale**: What happens at ten times the current alias count? Does verification stay usable, and does the answer stay reproducible?
- **Security**: Where do credentials appear in an alias's supporting artifacts, and can any reach a log, a verdict, or an evidence file?
- **User confusion**: Where could an operator conclude a capability is available when it is not — and does any surface still allow that?
- **Data integrity**: If validation is interrupted, can a partial verdict be recorded and later read as complete?
- **Backwards compatibility**: What happens to aliases created before this feature, and is any operator-visible capability lost?

## Open Questions

| # | Question | Status | Resolution |
|---|----------|--------|------------|
| Q1 | For the 19 aliases that are advertised but unusable: repair them so they work, or withdraw them so the list is honest? | Resolved 2026-09-08 | **Repair all 19.** Operator decision. Nothing is withdrawn, so no operator-visible capability is lost (FR-018 is satisfied by not exercising it). The residual risk was stated when the decision was taken and is accepted: if an alias proves genuinely un-repairable — retired provider, absent credential, dead endpoint — repairing "all" is not achievable for it, and it would keep presenting as available. That case is NOT to be absorbed silently; it returns as a fresh decision (repair differently, or withdraw with confirmation). |
| Q2 | Does a model that answers a task correctly but in an unexpected style count as usable? | Resolved 2026-09-08 | **Yes — correctness is the bar, form is not.** A model that solves the task in its own style is usable. This does not weaken anything: the answer must still be right, and a canned or stubbed reply still fails because it is not correct for a prompt it could not have anticipated. Expected effect on the measured baseline is to move models blocked only on answer STYLE into the ready set; models blocked for other reasons are unaffected. |
| Q3 | Does scope cover every model the hosts could serve, or only those already exposed? | Resolved 2026-09-08 | **Currently exposed, plus a documented and tested path to add more.** Bounds the work to a finishable set while ensuring a later addition is a routine operation rather than a fresh project. Enumerating everything the hosts *could* serve was rejected as unbounded — that set changes whenever a host's inventory changes, so it has no stable definition of done. |

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST NOT present a capability as available unless it can be used. Every advertised entry either completes a round trip or is shown with its unusable state and reason.
- **FR-002**: System MUST keep an alias and its supporting configuration consistent — it MUST NOT be possible to end in a state where one exists without the other, through any path that creates, restores, or refreshes them.
- **FR-003**: System MUST select how it talks to a model from that model's declared protocol, never inferred from the shape of its address.
- **FR-004**: System MUST report how old every verdict is, and MUST visibly mark a verdict older than the freshness horizon as no longer current.
- **FR-005**: System MUST distinguish an unknown verdict age from a recent one, and MUST NOT present unknown as recent.
- **FR-006**: System MUST produce identical verdicts across repeated runs against unchanged state.
- **FR-007**: System MUST base the default verdict on evidence that does not vary with a model's run-to-run behaviour; where a live sample is wanted, it MUST be reported separately as an occasional observation, never as the verdict.
- **FR-008**: System MUST base every claim of working on captured evidence. Configuration alone, absence of an error, or the presence of a name MUST NOT constitute proof.
- **FR-009**: System MUST distinguish, in its exit status, "the engine is unreachable" from "the operation ran and found nothing" from "the operation failed".
- **FR-010**: System MUST work in projects that have not adopted the shared governance corpus, and where a capability genuinely cannot be provided MUST say so plainly, naming what is unavailable, what still works, and how to supply it.
- **FR-011**: System MUST activate only the capabilities needed for current work, list the rest as available-but-inactive, and restore any of them in a single command that takes effect without restarting the session.
- **FR-012**: System MUST honour an explicit opt-out across session restarts; an opt-out MUST NOT be silently undone by any routine refresh.
- **FR-013**: System MUST make the restore path reachable without the operator knowing an installation-specific location.
- **FR-014**: System MUST include code owned by the team in its navigation index and exclude vendored third-party code.
- **FR-015**: System MUST report an empty or unavailable index as such, never as an absence of matches.
- **FR-016**: Every acceptance criterion MUST have a paired check that provably fails when the behaviour it describes is broken. A check that still passes when its subject is broken MUST be treated as a defect in the check.
- **FR-017**: Every measuring instrument used to establish a verdict MUST itself be validated against a known-good and a known-bad case, and MUST fail on the known-bad one.
- **FR-018**: System MUST NOT withdraw an operator-visible capability without explicit operator confirmation.
- **FR-019**: System MUST NOT expose a credential in any verdict, log, evidence file, or diagnostic message.
- **FR-020**: System MUST bring every currently-advertised alias to a usable state rather than withdrawing it (per Q1). Where an alias proves genuinely un-repairable, the system MUST surface it as a decision requiring operator input, and MUST NOT leave it presenting as available in the meantime.
- **FR-021**: System MUST judge a model ready on whether its answer is correct, not on whether the answer matches an expected form (per Q2). A reply that could have been produced without processing the request MUST still fail.
- **FR-022**: System MUST provide a documented, tested path for exposing an additional model, such that adding one is a routine operation (per Q3).

### Key Entities

- **Alias**: The name an operator uses to select a model. Carries the model's identity, how to reach it, which protocol it speaks, and whether it is currently usable.
- **Verdict**: A recorded judgement about one alias. Carries the outcome, the moment it was established, its age, and a pointer to the evidence behind it.
- **Evidence**: The captured material a verdict rests on. Must be sufficient for a second party to reach the same conclusion without rerunning anything.
- **Capability**: An extension the operator can activate or deactivate, in exactly one of two states, with a listed path back from inactive.
- **Catalogue**: The complete inventory of capabilities with their states — the answer to "what could I use here".

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of aliases presented as available complete a round trip. Measured baseline: 8 of 27 (30%).
- **SC-002**: Repeated validation against unchanged state produces identical verdicts across at least 10 consecutive runs.
- **SC-003**: An operator can determine whether a given model is usable without invoking it and without waiting on the network.
- **SC-004**: 100% of working claims cite captured evidence a second party can inspect.
- **SC-005**: Every inactive capability is restorable in one command, effective without restarting the session.
- **SC-006**: Every entry point behaves correctly in a project without the governance corpus — either working, or refusing with a message naming what is unavailable. Zero silent no-ops.
- **SC-007**: Deliberately breaking any behaviour this specification requires causes at least one check to fail. Zero behaviours are protected only by checks that cannot detect their absence.
- **SC-008**: A search for a symbol defined only inside an owned submodule returns it; a search over vendored third-party code returns nothing from it.
- **SC-009**: No credential appears in any verdict, log, evidence file, or diagnostic message.
- **SC-010**: An operator new to the system can tell, from the presented list alone, which models they can use — without trying them.

## Assumptions

- The operator is a developer or agent working through a command-line interface, not an end user of a hosted product.
- "Usable" means a round trip completes and returns model output. Quality of that output is out of scope except where a check needs a correct answer to distinguish real output from a canned one.
- The reference host's measured state (8 of 27 aliases usable; 3 of 7 models not yet meeting the readiness bar) is the current baseline, not a target already met.
- Model hosts may be unreachable at validation time. That is an honest unavailable, not a failure of this feature, provided it is reported as such.
- Freshness horizon defaults to the horizon the system already applies to comparable data rather than a newly chosen number, and remains overridable.
- Q1 was put to the operator and answered **repair all 19** on 2026-09-08, so no capability is withdrawn by this feature and FR-018's confirmation requirement is not exercised. The accepted residual — an alias that cannot in fact be repaired — returns as a fresh decision rather than being absorbed.
- Capability catalogue size is expected in the high hundreds; active count at any moment is expected to be a small fraction.
- Existing aliases predating this feature are in scope: they must reach a correct state, and none may lose capability without confirmation.

## Brainstorm Log

<!-- Maintained by the brainstorm command. No sessions recorded yet. -->
