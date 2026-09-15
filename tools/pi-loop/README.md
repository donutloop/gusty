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
5. Re-executes `AGENTS.md` and repeats **forever**.

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

## Auth

`pi-loop` connects to a local pi agent via the SDK; the SDK needs a model
provider and API key. Export the provider key (`ANTHROPIC_API_KEY`, etc.) in
your env, or set `PI_MODEL` to select a model.

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
