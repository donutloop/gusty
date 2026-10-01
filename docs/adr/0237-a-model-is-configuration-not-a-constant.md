# ADR 0237 — a model is configuration, not a constant

Status: accepted. Toolchain cycle, not a language cycle: it changes how **`tools/pi-loop`** (the
AGENTS.md loop driver built on the pi SDK) chooses the model it drives the loop with, and how an agent
or script can ask that question without reading source. Cites: ADR 0233 (a construct is implemented,
refused, or absent — never "it depends who asks", applied here to the driver's own configuration),
ADR 0211 (one event, one exit code — `--describe` exits 0/1 on a decision, not on a vibe), ADR 0166 /
0227 (single source of truth: the endpoint definition lives in exactly one file).

## What had been measured

`tools/pi-loop/pi-loop.mjs` carried its endpoint in source:

```js
const PROVIDER_CFG = { providers: { "local-vllm": { baseUrl: "http://localhost:8000/v1", … } },
                       models:  { "deepseek-v4-flash": { … } } };
const MODEL = "deepseek-v4-flash";
```

Meanwhile the repo already had the operator-facing configs in `setup/` — and `setup/pi_qwen3.8-flash-next.json`
had moved the loop's real target to `http://localhost:8888/v1`, model `qwen3.8-flash-next`
(`max_model_len` 262144, vLLM, thinking on by default, `reasoning_effort` accepted only as
`low|medium|xhigh`). The hard-coded pair was stale in three independent ways: dead port, dead model id,
and hard-coded `contextWindow: 524288` / `maxTokens: 131072` that no server behind this config has ever
had. Pointing the loop at a new model meant editing the driver.

Three more defects fell out of reading the code against the SDK rather than against memory:

1. **The written `models.json` was invalid, and quietly so.** pi validates `models.json` with a closed
   TypeBox object — only `providers` and `modelOverrides` are legal roots — and `ModelConfig.load`
   rejects the **whole file** on any unknown key. pi-loop wrote a top-level `models` map next to
   `providers`, so the file it proudly published was discarded wholesale by the SDK (`getError()` was
   never even printed), and every run silently ran on the hand-built inline model instead.
2. **The inline model duplicated pi's job.** It hard-coded `reasoning: true`, a full `thinkingLevelMap`,
   `supportsReasoningEffort: true`, `input: ["text","image"]`, cost, and the two window numbers — a
   second, drifting copy of exactly what `ModelRuntime` composes from `models.json`. With
   `supportsReasoningEffort: true` it sends `reasoning_effort: "max"`, which the current endpoint answers
   with **HTTP 400** (`Unexpected reasoning effort max. Supported types are xhigh (default), medium, and low.`).
3. **A parent pi session could hijack the loop.** pi exports `PI_MODEL` / `PI_PROVIDER` into every shell
   command it runs (`docs/environment-variables.md`), so a pi-loop started from inside a pi session read
   its parent's model out of the environment and, when the config disagreed, died with
   `model "…" is not in setup/…`. Measured directly: `env | grep PI_` inside a session shows both set.

## The decision

**`tools/pi-loop/models-config.mjs` owns configuration; `pi-loop.mjs` owns the loop.**

1. **The config is a file in `setup/`, never a constant.** It uses pi's own `models.json` schema —
   `{ "providers": { "<id>": { baseUrl, api, apiKey, compat, "models": [ { id, contextWindow, … } ] } } }`
   — so the same file configures the human `pi` CLI in that directory and the agent driver. Discovery:
   `--models-config=` > `PI_LOOP_MODELS_CONFIG`/`PI_MODELS_CONFIG` > newest **named** profile
   `setup/pi_*.json` (`pi.json` is the generic fallback, ranked last regardless of mtime) in
   `PI_SETUP_DIR`, `<cwd>/setup`, `tools/../setup`. Selecting a model is `--provider/--model` >
   `PI_LOOP_PROVIDER/PI_LOOP_MODEL` > first declared model.
   **Adding a model is a file drop; the driver does not change.**
2. **Publish, then ask pi.** The config is merged into `<agentDir>/models.json` (`providers` merged,
   foreign providers and `modelOverrides` preserved, the legacy top-level `models` key dropped), and the
   model is resolved with `ModelRuntime.getPhysicalModel(provider, id)` and handed back to
   `createAgentSession` together with that same runtime — so baseUrl, api, compat, cost and the thinking
   clamp come from pi, once. The inline object survives only as a documented fallback for an
   unresolvable model.
3. **Never inflate a limit nobody stated.** `modelLimits` reports where each number came from
   (`setting` | `config` | `endpoint` | `default`) and `applyLimits` replaces pi's resolved value only for
   a stated origin; an invented 262144/131072 pair never overwrites pi's conservative 128000/16384.
   Rationale is the vLLM failure mode: a server rejects `prompt_tokens + max_tokens > max_model_len`, so
   an over-generous output budget turns a long round into an HTTP 400 late in the game.
4. **`PI_LOOP_<NAME>` is the clash-free namespace**, `--flag` beats it, plain `PI_<NAME>` is honored only
   where pi itself does not use it: inside a pi session (`PI_CODING_AGENT=true`) `PI_MODEL`/`PI_PROVIDER`
   are **ignored and reported** rather than obeyed.
5. **Fail before the round, not after it.** A `GET <baseUrl>/models` pre-flight exits 1 with an actionable
   message when the server is down (it also surfaces `ECONNREFUSED` from `fetch`'s `cause`, which is
   otherwise an unusable `fetch failed`) or does not serve the chosen id; its reported `max_model_len`
   feeds the context window when the config omits one.
6. **Machine path first-class**: `--describe` prints a JSON document on stdout (human banners go to
   stderr) with the resolved config path, provider/model, endpoint, limits *and their origins*, `compat`,
   available ids, ignored inherited settings, and the probe result; `--help` prints the flag/env
   reference; `--dry-run` runs one tool-less connectivity round (`noTools: "all"`, no state write) so the
   whole chain — config → `models.json` → SDK session → local endpoint — can be proved without touching
   the repo.

## Alternatives rejected

- **Keep the constant and just edit the two lines.** Cheapest diff, and it re-creates the defect: the
  next config drop in `setup/` goes stale against the driver again, and nothing tests the pairing.
- **Read `setup/pi_qwen3.8-flash-next.json` by name.** Deterministic today, and a rename (which is
  exactly what just happened, `pi.json` → `pi_qwen3.8-flash-next.json`) breaks it. Ranked discovery with
  an explicit override is the same code with a future-proof default.
- **Let the driver keep hard-coding `compat`/`thinkingLevelMap`.** It is pi's data model duplicated in
  Go-tool source; the 400 above is what duplication costs. The config declares compat (`supportsDeveloperRole`,
  `supportsReasoningEffort: false`) and pi composes the rest; a model that wants pi-driven thinking sets
  `"reasoning": true` + `thinkingLevelMap` in the config, not in the driver.
- **Trust the SDK's `models.json` error silently, as before.** `ModelRuntime.getError()` is now printed;
  an invalid config is visible in one line instead of changing the run's behavior invisibly.
- **Rename the env vars outright.** `PI_LOOP_*` became the canonical spelling and wins, but the old
  `PI_*` names still work outside a pi session, because that is how existing operators' shells and CI
  already call the loop.
- **Skip the probe, "let the request fail".** The first signal would then arrive after minutes of agent
  work and inside a stream error; a 5 s pre-flight with a typed message is a cheaper teacher.
  `PI_SKIP_MODEL_CHECK=1` keeps the escape hatch.

## Verification

`tools/pi-loop/models-config.test.mjs` (13 cases, `node --test tools/pi-loop/*.test.mjs`): discovery
rank (named profile over newer `pi.json`), pinned paths, unusable-config messages, the shipped
`setup/pi_qwen3.8-flash-next.json` resolving to `local-vllm/qwen3.8-flash-next`, selector typos, limit
precedence and origins, `applyLimits` non-inflation and non-mutation, the `models.json` merge incl. the
legacy-key drop, the `PI_MODEL`/`PI_PROVIDER` ignore-and-report rule inside a pi session, and the probe's
reachable / wrong-model / HTTP-500 / ECONNREFUSED / timeout branches. End to end:
`node tools/pi-loop/pi-loop.mjs . --dry-run` answers `PI_LOOP_OK` on the new endpoint, and `--describe |
jq` parses as pure JSON.
