# pi-loop

A CLI built on the **pi SDK** (`@earendil-works/pi-coding-agent`) that connects
to a **local pi agent** session and drives the **AGENTS.md loop**, with
**resume-on-stop** semantics.

## What it does

Every round:

1. Loads `AGENTS.md` from the repo.
2. Prompts the local pi agent to read it and follow its loop **strictly**:
   pick the next mission feature → implement across the stack (code, machine
   path, tests, docs, CHANGELOG) → run tests until green → commit → push.
3. Waits for the agent turn to complete (`agent_end`).
4. Verifies the round (`git status` / `git rev-parse HEAD`) and **persists
   progress** to `.pi-loop-state.json`.
5. **Discards** the round's session and starts a brand-new one (new session
   id + fresh session log file) for the next round.
6. Re-executes `AGENTS.md` and repeats **forever**.

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

## Provider config

`pi-loop` connects using **only** the embedded provider config — a local
OpenAI-compatible vLLM endpoint:

```json
{ "providers": { "local-vllm": { "baseUrl": "http://localhost:8000/v1", "api": "openai-completions", "apiKey": "dummy" } } }
```

On startup it writes/merges this into `<agentDir>/models.json` and creates the
SDK session with `model: "deepseek-v4-flash"`.

## Requirements

- The pi SDK installed (globally or under `PI_SDK_PATH`).
- The pi CLI's agent config resolvable for `createAgentSession({cwd, agentDir})`
  (defaults are auto-configured from the local pi config).

## Usage

```sh
node tools/pi-loop/pi-loop.mjs <project-dir>            # loop forever
node tools/pi-loop/pi-loop.mjs <project-dir> --once      # one round
node tools/pi-loop/pi-loop.mjs <project-dir> --rounds=3  # three rounds
node tools/pi-loop/pi-loop.mjs <project-dir> --force-reset   # ignore saved progress
```

Env:

| var            | default |
|----------------|---------|
| `PI_SDK_PATH`  | global npm pi SDK `dist/index.js` |
| `PI_AGENT_DIR` | the project dir (the `cwd` passed to the SDK) |
| `PI_MODEL` | optional model id passed to the SDK session (otherwise SDK defaults) |

## Machine path

`pi-loop` is itself a CLI: `tools/pi-loop/package.json` exposes the `pi-loop`
bin. Run it with `node` directly, or `npx pi-loop` once installed.

## Loop contract (AGENTS.md)

The agent reads the repo's `AGENTS.md` each round and must follow its loop
exactly — including committing and pushing before reporting completion.
