# pi-loop

A CLI built on the **pi SDK** (`@earendil-works/pi-coding-agent`) that connects
to a **local pi agent** session and drives the **AGENTS.md loop**, with
**resume-on-stop** semantics.

## What it does

Every round:

1. Loads the **model config** (see below) and opens a local pi agent session on it.
2. Loads `AGENTS.md` from the repo.
3. Prompts the local pi agent to read it and follow its loop **strictly**:
   pick the next mission feature → implement across the stack (code, machine
   path, tests, docs) → run tests until green → commit → push.
4. Waits for the agent turn to complete (`agent_end`).
5. Verifies the round (`git status` / `git rev-parse HEAD`) and **persists
   progress** to `.pi-loop-state.json`.
6. **Discards** the round's session and starts a brand-new one (new session
   id + fresh session log file) for the next round.
7. Re-executes `AGENTS.md` and repeats **forever**.

## Model config — comes from `setup/`, never hard-coded

pi-loop carries **no** endpoint or model id. It reads a config written in the
exact format pi itself reads from `<agentDir>/models.json`, and the current one
lives next to the other boot configs:

```jsonc
// setup/pi_qwen3.8-flash-next.json
{
  "providers": {
    "local-vllm": {
      "baseUrl": "http://localhost:8888/v1",
      "api": "openai-completions",
      "apiKey": "dummy",
      "compat": { "supportsDeveloperRole": false, "supportsReasoningEffort": false },
      "models": [ { "id": "qwen3.8-flash-next", "contextWindow": 262144 } ]
    }
  }
}
```

On startup pi-loop:

1. **Discovers** the config: `--models-config=PATH` > `PI_LOOP_MODELS_CONFIG` /
   `PI_MODELS_CONFIG` > the newest *named* profile `setup/pi_*.json`
   (`setup/pi.json` is the generic fallback) in `PI_SETUP_DIR`, `<cwd>/setup`,
   then `tools/../setup`.
2. **Selects** provider + model: `--provider/--model` >
   `PI_LOOP_PROVIDER`/`PI_LOOP_MODEL` > the first model of the first provider.
3. **Publishes** it to `<agentDir>/models.json` (merging, preserving unrelated
   providers and `modelOverrides`, and dropping the legacy top-level `models`
   key — pi rejects the whole file when an unknown root key is present).
4. **Resolves** the model through the SDK's own `ModelRuntime`, so pi fills in
   every default (`baseUrl`, `api`, `compat`, `cost`, thinking clamp). If the
   runtime cannot resolve it, pi-loop falls back to an inline model definition.
5. **Probes** `<baseUrl>/models` and fails fast (exit 1) with an actionable
   message when the server is down or does not serve the selected id; the
   reported `max_model_len` is also used when the config omits `contextWindow`.

So **pointing the loop at a new model is a file drop in `setup/`** — no code
change, and the same file configures the human-facing `pi` CLI in that directory.

Thinking: the config decides. `qwen3.8-flash-next` sets
`supportsReasoningEffort: false`, so pi-loop never sends `reasoning_effort`
(that endpoint rejects every value except `low|medium|xhigh`) and the server
applies its own default effort. To let pi drive the level instead, add
`"reasoning": true` and a `thinkingLevelMap` to the model entry and pick a level
with `PI_LOOP_THINKING_LEVEL` (default `max`).

### Inside another pi session

pi exports `PI_MODEL` / `PI_PROVIDER` into every command it runs, so a pi-loop
started from inside a pi session would otherwise inherit the *parent* session's
model. pi-loop ignores those two variables when `PI_CODING_AGENT=true` and says
so on stderr; use `--model=ID` or `PI_LOOP_MODEL` to be explicit. Any setting
also accepts the clash-free `PI_LOOP_<NAME>` spelling, which always wins.

## Fresh session per round

Each round runs in its **own brand-new pi session** — pi-loop never resumes or
continues a prior session. Before the round's prompt it creates a fresh
`SessionManager` (which runs `newSession()`, producing a new session id and a
new `<agentDir>/sessions/*.jsonl` log), prompts, then **disposes** that session
once the round completes. The previous round's session context and persisted
log are fully discarded, so no context or history carries across rounds.

## Stop / resume semantics

If pi-loop stops — **SIGINT, an error, or the rounds target is reached** — it
persists `{ round, lastCommit }` to `<cwd>/.pi-loop-state.json` before exiting.

On the next run:

- **If there IS committed progress and `lastCommit` matches `HEAD`** with a
  clean tree → pi-loop **RESUMES at the next round** (continues with current
  progress).
- **If there is NO progress** (no state file, `lastCommit != HEAD`, or a dirty
  working tree) → pi-loop **RE-EXECUTES AGENTS.md** and starts fresh at round 1.

In other words: *when a round completes, execute AGENTS.md and follow its loop;
when pi-loop stops, restart where it left off — or re-run AGENTS.md if no
progress exists yet.*

## Usage

```sh
node tools/pi-loop/pi-loop.mjs <project-dir>                 # loop forever
node tools/pi-loop/pi-loop.mjs <project-dir> --once          # one round
node tools/pi-loop/pi-loop.mjs <project-dir> --rounds=3      # three rounds
node tools/pi-loop/pi-loop.mjs <project-dir> --force-reset   # ignore saved progress
node tools/pi-loop/pi-loop.mjs <project-dir> --dry-run       # connectivity round, tools disabled, no state
node tools/pi-loop/pi-loop.mjs --help                        # full flag list
```

| flag | meaning |
|------|---------|
| `[cwd]` | project directory to drive (default `process.cwd()`) |
| `--rounds=N` | number of rounds (default `Infinity`) |
| `--once` | shorthand for `--rounds=1` |
| `--force-reset` | ignore `.pi-loop-state.json`, restart at round 1 |
| `--models-config=PATH` | pin the models config instead of auto-discovery |
| `--provider=ID` | provider selector inside that config |
| `--model=ID` | model selector inside that config |
| `--dry-run` | one connectivity round with **no tools**, no state writes, no git checks |
| `--describe` | print the resolved configuration as JSON on stdout and exit |
| `--help` | usage |

Env (each also accepted as `PI_LOOP_<NAME>`, which wins; `--flag` beats both):

| var | default |
|-----|---------|
| `PI_SDK_PATH` | global npm pi SDK `dist/index.js` |
| `PI_AGENT_DIR` | the project dir (the `cwd` passed to the SDK) |
| `PI_SETUP_DIR` | `<cwd>/setup`, then `tools/../setup` |
| `PI_MODELS_CONFIG` | auto-discovered `setup/pi*.json` |
| `PI_PROVIDER` / `PI_MODEL` | first provider / first model of the config (ignored inside a pi session) |
| `PI_THINKING_LEVEL` | `max` (clamped to what the model supports) |
| `PI_CONTEXT_WINDOW` / `PI_MAX_TOKENS` | from config, else endpoint `max_model_len` / half the window |
| `PI_SKIP_MODEL_CHECK=1` | skip the `<baseUrl>/models` pre-flight |
| `PI_MODEL_CHECK_TIMEOUT_MS` | `5000` |
| `PI_LOOP_DELAY_SECONDS` | `60` between rounds |

## Machine path

- `--describe` prints a JSON document (stdout only — human banners go to
  stderr) describing exactly what a run would use: resolved config path,
  provider, model, `baseUrl`, `api`, limits, `compat`, thinking level, the
  available model ids, inherited settings that were ignored, and the endpoint
  probe result. Exit `0` on success, `1` with a `pi-loop: fatal …` message on
  stderr when the config, model id, or endpoint is unusable.

  ```sh
  node tools/pi-loop/pi-loop.mjs . --describe | jq -r .model
  ```
- `--help` prints the flag/env reference.
- `tools/pi-loop/package.json` exposes the `pi-loop` bin; run with `node`
  directly or `npx pi-loop` once installed.

## Tests

```sh
node --test tools/pi-loop/*.test.mjs      # or: npm test --prefix tools/pi-loop
```

`models-config.test.mjs` covers the config layer (discovery order, provider/model
selection and typo messages, limit precedence, the `models.json` merge including
the legacy-key trap, the inline-model fallback, the endpoint probe incl. timeout).
For the full chain (config → `models.json` → SDK session → local endpoint) run:

```sh
node tools/pi-loop/pi-loop.mjs . --dry-run     # expects "PI_LOOP_OK" from the model
```

## Requirements

- The pi SDK installed (globally or under `PI_SDK_PATH`).
- The local model server up on the `baseUrl` in the chosen `setup/` config
  (see `setup/boot_agent.sh`); `--describe` tells you whether it is reachable.

## Loop contract (AGENTS.md)

The agent reads the repo's `AGENTS.md` each round and must follow its loop
exactly — including committing and pushing before reporting completion.
