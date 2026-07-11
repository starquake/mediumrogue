# Inventory System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 8 typed gear slots (paper-doll), a 4-entry backpack with consumable stacks, drop, prompt-based pickup with slot gating, drink — plus the starter content (leather armor, headband, healing potion).

**Architecture:** `itemType` drives slot derivation and class-shaped weapon slots; entity storage becomes `equipped map[slot]` + `backpack[4]` with stacks; all inventory actions follow the free-outside/turn-inside rule; pickup becomes a client prompt + server-validated intent; snapshot version bumps.

**Spec:** `docs/superpowers/specs/2026-07-11-inventory-slots-design.md` — binding. **Prerequisite:** playtest batch 3 merged (this rebuilds the gear panel it touched); merge `origin/main` into this branch before starting and after batch 3 lands.

## Global Constraints

- `make check` green per commit; `make protocol` after protocol changes; FEATURES.md/STATUS/plan/content-guide updates ride this PR (CLAUDE.md convention).
- Combat semantics unchanged: closeDefFor/rangedDefFor keep their contracts (staff bonks, wand never, fighter thrown empty ⇒ no ranged) — every existing combat test passes with at most storage-model re-derivations, never weakened.
- Task 5 (client UI) starts ONLY after the controller relays mockup approval; tasks 1–4 are server-side and proceed immediately.
- Go style `.claude/rules/go-style.md`; TDD per task.

---

### Task 1: Taxonomy + entity storage model
**Files:** `internal/game/items.go` (itemType consts, type→slot, wearableBy set — ITEM wearability, characters stay single-class), `content.go` (re-type every existing def), `world.go` (entity: `equipped map[string]int64`, `backpack [4]backpackEntry`; Join grants defaults into the class-shaped weapon slots), validation (type/slot/classes coherence; consumable heal>0), tests.
**Produces:** `itemType` consts; `slotForType(t) string`; `weaponSlotsFor(class) [2]itemType`; `backpackEntry{inst itemInstance; count int}` (count≥1; >1 only for consumables); entity helpers `equippedDefIn(slot)`, `freeBackpackIndex()`, `stackIndexFor(defID)`.
- [ ] TDD; combat resolution reads the new storage (closeDefFor → the class's melee-ish slot: melee for fighter/rogue, staff for mage; rangedDefFor → thrown/ranged/wand by class). ALL existing game tests green. Commit.

### Task 2: Actions — equip/unequip/drop/pickup/drink intents
**Files:** `internal/protocol/protocol.go` (intent kinds unequip/drop/pickup/drink; ItemView += type,count, slot-keyed equipped; GroundItemView += type) + regen; `world.go` (queue/immediate paths per the free-outside/turn-inside rule — mirror queueEquipLocked's shape; pickup validates merge>free-entry>reject with typed errors; auto-pickup REMOVED; drop spawns ground item at own hex; drink heals+decrements), `internal/server/api.go` error mapping; tests incl. the exact rejection error text.
- [ ] TDD each action; the pickup priority and the bubble turn-consumption rules pinned. Commit per action group.

### Task 3: Starter content + guide vocabulary
**Files:** `content.go` (+leather-armor, headband-of-learning, healing-potion per the spec table; potions into rat/wolf tables low weight), `docs/rule-based-content-design.md` (armor/consumable card fields; heal is a def field not a pipeline event), pin tests.
- [ ] Cards work through the live pipeline (take-damage −1; earn-XP ×1.05) — equivalence tests mirroring the species-card style. Commit.

### Task 4: Persistence + integration
**Files:** `internal/game/snapshot.go` (DTOs for equipped map/backpack/stacks; `snapshotVersion++`), archive records (same new shape), `test/integration/inventory_test.go` (drop→walk→prompt-accept/reject/full over HTTP; drink; restart round-trip with equipped+stacked state).
- [ ] Round-trip equality incl. stacks; version bump verified (old snapshot → preserved-aside + fresh). Commit.

### Task 5 (GATED on mockup approval): Paper-doll client + e2e
**Files:** `client/src/gear/` rewrite (PaperDoll.tsx + Backpack + PickupPrompt per the approved mockup; store reworked to slot-keyed state), `main.ts` wiring (prompt queue driven by "standing on ground items" from bundles; window.game.equipped/backpack/pendingPickupPrompt), `index.html` CSS, `render/items.ts` unchanged, e2e: gear.spec rewritten for slots; new pickup-prompt + stacking + backpack-full-feedback specs.
- [ ] Screenshot the final panel (drive the real binary) and link it in the PR. Full `make e2e`. Commit.

### Task 6: Docs
- [ ] FEATURES.md (inventory section rewrite: slots table, backpack, stacks, pickup flow, new intents, starter content), STATUS.md, plan doc §0 (taxonomy recorded) + §9 note. Full `make check` + `make e2e`. Commit; `gh pr create` (“Inventory system — slots, backpack, drop & pickup”), body per task + screenshot + the world-reset note (version bump). Do NOT merge.

---
## Self-Review
- Spec coverage: taxonomy/storage (T1), five actions + wire (T2), content (T3), persistence (T4), UI+e2e (T5), docs (T6). Mockup gate honored via T5's explicit hold. ✔
- Consistency: backpackEntry defined T1, serialized T4; intent names protocol-side T2 only; combat-seam contract named in constraints. ✔
