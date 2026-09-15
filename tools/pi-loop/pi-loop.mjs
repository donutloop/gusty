#!/usr/bin/env node
/**
 * pi-loop — a CLI built on the pi SDK that connects to a LOCAL pi agent
 * session and drives the AGENTS.md loop, with resume-on-stop semantics.
 *
 * Each round:
 *   1. Prompt the pi agent to read AGENTS.md and strictly follow its loop
 *      (pick the next mission feature, implement across the stack, add the
 *      machine path, add tests, update docs + CHANGELOG, run tests, commit,
 *      push).
 *   2. Wait for the agent turn to complete (agent_end).
 *   3. Verify the round (git status / last commit) and persist progress.
 *   4. Repeat.
 *
 * Stop/resume:
 *   If pi-loop stops (SIGINT, error, or the rounds target is reached) it
 *   persists {round, lastCommit} to <cwd>/.pi-loop-state.json.
 *   On the next run:
 *     - if there IS committed progress and lastCommit matches HEAD, pi-loop
 *       RESUMES at the next round (continues with current progress).
 *     - if there is NO progress (no state file, or lastCommit != HEAD, or a
 *       dirty working tree), pi-loop RE-EXECUTES AGENTS.md from round 1.
 *
 * Usage:
 *   node pi-loop.mjs [cwd] [--rounds N] [--once] [--force-reset]
 * Env:
 *   PI_SDK_PATH  absolute path to the pi SDK dist/index.js
 *   PI_AGENT_DIR agent directory (default: cwd)
 */
import { createRequire } from "node:module";
import { spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const require = createRequire(import.meta.url);
const DEFAULT_SDK = "/home/donutloop/.npm-global/lib/node_modules/@earendil-works/pi-coding-agent/dist/index.js";
const sdkPath = process.env.PI_SDK_PATH || DEFAULT_SDK;
const { createAgentSession } = require(sdkPath);

const args = process.argv.slice(2);
const cwd = args.find(a => !a.startsWith("--")) || process.cwd();
const roundsArg = args.find(a => a.startsWith("--rounds=")) ?? "Infinity";
const rounds = roundsArg === "Infinity" ? Infinity : parseInt(roundsArg.slice(9), 10);
const once = args.includes("--once") ? 1 : rounds;
const forceReset = args.includes("--force-reset");
const agentDir = process.env.PI_AGENT_DIR || cwd;

const STATE_FILE = path.join(cwd, ".pi-loop-state.json");

// pi-loop connects using ONLY this provider config (a local OpenAI-compatible
// vLLM endpoint). The SDK's ModelRuntime reads <agentDir>/models.json; we
// write/merge this provider + model there so createAgentSession resolves it.
const PROVIDER_CFG = {
  providers: {
    "local-vllm": {
      baseUrl: "http://localhost:8899/v1",
      api: "openai-completions",
      apiKey: "dummy"
    }
  },
  models: {
    "deepseek-v4-flash": {
      provider: "local-vllm",
      name: "deepseek-v4-flash",
      baseUrl: "http://localhost:8899/v1",
      api: "openai-completions"
    }
  }
};
const MODEL = "deepseek-v4-flash";
const MODELS_JSON = path.join(agentDir, "models.json");

function bootstrapModelsJson() {
  let existing = {};
  try { existing = JSON.parse(fs.readFileSync(MODELS_JSON, "utf8")); } catch {}
  existing.providers = Object.assign({}, existing.providers, PROVIDER_CFG.providers);
  existing.models = Object.assign({}, existing.models, PROVIDER_CFG.models);
  fs.writeFileSync(MODELS_JSON, JSON.stringify(existing, null, 2));
  console.log("pi-loop: wrote provider config to " + MODELS_JSON + " (local-vllm -> deepseek-v4-flash)");
}

bootstrapModelsJson();

const agentsMd = path.join(cwd, "AGENTS.md");
const loopContract = fs.existsSync(agentsMd) ? fs.readFileSync(agentsMd, "utf8") : "";

function git(argsArr) {
  return new Promise((resolve) => {
    const p = spawn("git", argsArr, { cwd });
    let out = "";
    p.stdout.on("data", d => (out += d));
    p.stderr.on("data", d => (out += d));
    p.on("close", () => resolve(out.trim()));
  });
}

function loadState() {
  try {
    return JSON.parse(fs.readFileSync(STATE_FILE, "utf8"));
  } catch {
    return null;
  }
}

function saveState(state) {
  try {
    fs.writeFileSync(STATE_FILE, JSON.stringify(state, null, 2));
    console.log(`pi-loop: persisted progress ${JSON.stringify(state)} -> ${STATE_FILE}`);
  } catch (e) {
    console.error("pi-loop: failed to persist state:", e.message);
  }
}

async function verifyRound(round) {
  const status = await git(["status", "--porcelain"]);
  const head = await git(["rev-parse", "HEAD"]);
  console.log(`pi-loop: [round ${round}] tree ${status ? "DIRTY" : "clean"} HEAD ${head.slice(0, 8)}`);
  return { clean: status === "", head };
}

async function runRound(session, round, maxRounds) {
  const prompt = [
    `Round ${round} of ${maxRounds === Infinity ? "∞" : maxRounds}. `,
    "Read AGENTS.md in this repo and follow its loop STRICTLY.",
    "Pick the next feature from the mission list. Implement it across the stack:",
    "1) code 2) machine/CLI path 3) unit tests 4) docs (language.md/operations.md/README) 5) CHANGELOG.md entry",
    "6) run the test suite until green 7) one commit with a clear message 8) push.",
    "Finish by reporting: the feature, the commit hash, and that it was pushed.",
  ].join("\n");

  console.log(`\n=== pi-loop: round ${round} — prompting local pi agent ===`);
  try {
    await session.prompt(prompt);
  } catch (e) {
    console.error(`\n=== pi-loop: prompt threw ===`);
    console.error(e?.stack || e);
  }
  console.log(`\n=== pi-loop: round ${round} agent turn complete ===`);
  const { clean, head } = await verifyRound(round);
  if (!clean) {
    console.warn(`pi-loop: round ${round} left an uncommitted working tree.`);
  }
  return head;
}

// Decide resume vs re-execute AGENTS.md.
let state = loadState();
let round = 1;
if (!forceReset && state) {
  const head = await git(["rev-parse", "HEAD"]);
  const dirty = await git(["status", "--porcelain"]);
  if (state.lastCommit && state.lastCommit === head && !dirty) {
    round = state.round;
    console.log(`pi-loop: RESUMING with current progress — continuing from round ${round} (HEAD ${head.slice(0, 8)})`);
  } else {
    console.log(`pi-loop: progress out of sync (state.lastCommit=${state.lastCommit?.slice(0, 8) ?? "none"}, HEAD=${head.slice(0, 8)}, dirty=${!!dirty}) — RE-EXECUTING AGENTS.md`);
    round = 1;
    state = null;
  }
} else {
  console.log("pi-loop: no committed progress — RE-EXECUTING AGENTS.md (fresh round 1)");
}

let session;
try {
  // createAgentSession expects a Model OBJECT (provider + id), not a bare id
  // string. The SDK resolves the full model (auth/endpoint) from
  // <agentDir>/models.json using model.provider / model.id, so a minimal
  // object with provider+id+name is enough (apiKey "dummy" lives on the
  // provider config written to models.json).
  // The SDK does NOT auto-fill baseUrl/api from models.json for an inline
  // model object — the openai-completions provider calls model.baseUrl.*
  // (detectCompat) and uses model.baseUrl as the endpoint, so they MUST be
  // present or every agent turn crashes with
  // "Cannot read properties of undefined (reading 'includes')".
  const providerCfg = PROVIDER_CFG.providers["local-vllm"];
  const model = {
    provider: "local-vllm",
    id: MODEL,
    name: MODEL,
    baseUrl: providerCfg.baseUrl,
    api: providerCfg.api,
    reasoning: false,
    thinkingLevelMap: {},
    input: ["text", "image"],
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    contextWindow: 524288,
    maxTokens: 131072,
    headers: {}
  };
  const created = await createAgentSession({ cwd, agentDir, model });
  session = created.session ?? created;
  console.log("pi-loop: connected to local pi agent session (" + agentDir + ")");
  session.subscribe((event) => {
    if (event.type === "message_update") {
      const delta = event.assistantMessage?.textDelta ?? event.delta ?? "";
      if (delta) process.stdout.write(delta);
    }
  });

  const target = once || rounds;
  const maxRounds = target === Infinity ? Infinity : target;
  while (round <= maxRounds) {
    const head = await runRound(session, round, maxRounds);
    state = { round: round + 1, lastCommit: head, finishedAt: new Date().toISOString() };
    saveState(state);
    console.log(`pi-loop: round ${round} completed; executing AGENTS.md loop for next round...`);
    round += 1;
  }
  session.dispose();
  console.log("pi-loop: finished rounds=" + (round - 1));
} catch (e) {
  console.error("pi-loop: fatal:", e.message);
  if (state) saveState(state);
  if (session) session.dispose();
  process.exit(1);
}

process.on("SIGINT", () => {
  console.log("\npi-loop: interrupted; persisting progress and disconnecting...");
  if (state) saveState(state);
  process.exit(130);
});
