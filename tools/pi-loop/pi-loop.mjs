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

// The pi SDK persists every session entry to a JSONL log file under
// <agentDir>/sessions/. We keep that file persistence AND mirror the exact
// same output to the console, so operators see everything the session sees.
const SDK_DIR = path.dirname(sdkPath);
const { SessionManager, getDefaultSessionDir } = require(path.join(SDK_DIR, "core/session-manager.js"));

// ---- human-readable console UI -------------------------------------------------
// The session log is still written to <agentDir>/sessions/*.jsonl exactly as the
// SDK produces it (we only wrap the append methods, never the file). What you see
// here is a clean, 2026-developer console: colored roles, wall-clock timestamps,
// compact token counts, and no raw JSON blobs.
const ansi = {
  reset: "\x1b[0m",
  bold: "\x1b[1m",
  dim: "\x1b[2m",
  cyan: "\x1b[36m",
  green: "\x1b[32m",
  yellow: "\x1b[33m",
  magenta: "\x1b[35m",
  blue: "\x1b[34m",
  gray: "\x1b[90m",
  red: "\x1b[31m",
};
const paint = (s, code) => (code ? `${code}${s}${ansi.reset}` : s);

function fmtTime(iso) {
  const d = iso ? new Date(iso) : new Date();
  const hh = d.getHours().toString().padStart(2, "0");
  const mm = d.getMinutes().toString().padStart(2, "0");
  const ss = d.getSeconds().toString().padStart(2, "0");
  return `${hh}:${mm}:${ss}`;
}

function fmtTokens(n) {
  if (n == null) return null;
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return String(n);
}
function fmtUsage(usage) {
  if (!usage) return null;
  const parts = [];
  const inTok = usage.input_tokens ?? usage.prompt_tokens;
  const outTok = usage.output_tokens ?? usage.completion_tokens;
  if (inTok != null) parts.push(`${fmtTokens(inTok)} in`);
  if (outTok != null) parts.push(`${fmtTokens(outTok)} out`);
  if (usage.total_tokens != null) parts.push(`${fmtTokens(usage.total_tokens)} total`);
  return parts.length ? `↗ ${parts.join(" · ")}` : null;
}

function roleBadge(role) {
  switch (role) {
    case "user": return paint("you", ansi.cyan);
    case "assistant": return paint("agent", ansi.magenta);
    case "toolResult": return paint("tool", ansi.yellow);
    case "system": return paint("system", ansi.gray);
    default: return paint(String(role), ansi.gray);
  }
}

function renderText(content) {
  if (content == null) return "";
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content
      .map((c) => {
        if (typeof c === "string") return c;
        if (c && typeof c === "object") {
          if (c.type === "text" || c.text) return String(c.text ?? "");
          if (c.type === "image" || c.image) return "[image]";
          try { return JSON.stringify(c); } catch { return "[object]"; }
        }
        return String(c);
      })
      .join("\n");
  }
  try { return JSON.stringify(content); } catch { return "[object]"; }
}

// One message rendered as a two-line block: a header line (timestamp + role
// badge + token usage) then the body indented beneath it.
function renderMessage(m) {
  const role = m?.role ?? "message";
  const ts = fmtTime(m?.timestamp);
  const body = renderText(m?.content).replace(/\n/g, "\n  ");
  const usage = fmtUsage(m?.usage);
  const head = `${paint(ts, ansi.dim)} ${roleBadge(role)}${usage ? `  ${paint(usage, ansi.dim)}` : ""}`;
  return `  ${head}\n  ${body}`;
}

// A small event line (no body) for non-message entries.
function renderEvent(badge, text) {
  return `  ${paint(fmtTime(), ansi.dim)} ${badge}${text ? `  ${text}` : ""}`;
}

/**
 * Build a SessionManager that writes to the session log file exactly like the
 * SDK default, but ALSO prints every entry to the console so the operator sees
 * the same output the session sees.
 */
function createConsoleMirrorSessionManager(cwd, agentDir) {
  const sm = SessionManager.create(cwd, getDefaultSessionDir(cwd, agentDir));
  const orig = {
    appendMessage: sm.appendMessage.bind(sm),
    appendThinkingLevelChange: sm.appendThinkingLevelChange.bind(sm),
    appendModelChange: sm.appendModelChange.bind(sm),
    appendCompaction: sm.appendCompaction.bind(sm),
    appendCustomEntry: sm.appendCustomEntry.bind(sm),
    appendSessionInfo: sm.appendSessionInfo.bind(sm),
    appendCustomMessageEntry: sm.appendCustomMessageEntry.bind(sm),
    appendLabelChange: sm.appendLabelChange.bind(sm),
  };

  sm.appendMessage = (message) => {
    console.log(renderMessage(message));
    return orig.appendMessage(message);
  };
  sm.appendThinkingLevelChange = (level) => {
    console.log(renderEvent(paint("thought", ansi.blue), String(level)));
    return orig.appendThinkingLevelChange(level);
  };
  sm.appendModelChange = (provider, modelId) => {
    console.log(renderEvent(paint("model", ansi.green), `${provider}/${modelId}`));
    return orig.appendModelChange(provider, modelId);
  };
  sm.appendCompaction = (summary) => {
    console.log(renderEvent(`${paint("context", ansi.yellow)} ${paint("compacted", ansi.bold)}`, renderText(summary)));
    return orig.appendCompaction(summary);
  };
  sm.appendCustomEntry = (customType, data) => {
    console.log(renderEvent(paint(String(customType), ansi.gray), typeof data === "string" ? data : renderText(data)));
    return orig.appendCustomEntry(customType, data);
  };
  sm.appendSessionInfo = (name) => {
    console.log(renderEvent(paint("session", ansi.green), String(name)));
    return orig.appendSessionInfo(name);
  };
  sm.appendCustomMessageEntry = (customType, content, display, details) => {
    console.log(renderEvent(paint(String(customType), ansi.gray), renderText(content)));
    return orig.appendCustomMessageEntry(customType, content, display, details);
  };
  sm.appendLabelChange = (targetId, label) => {
    console.log(renderEvent(paint("label", ansi.gray), `${targetId} -> ${label ?? "(cleared)"}`));
    return orig.appendLabelChange(targetId, label);
  };

  return sm;
}

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
      baseUrl: "http://localhost:8000/v1",
      api: "openai-completions",
      apiKey: "dummy"
    }
  },
  models: {
    "deepseek-v4-flash": {
      provider: "local-vllm",
      name: "deepseek-v4-flash",
      baseUrl: "http://localhost:8000/v1",
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

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
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
    "Read AGENTS.md in this repo and follow its instructions - follow the loop STRICTLY."
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

// Hold a reference to the currently-active round's session so SIGINT and the
// fatal-error handler can dispose it cleanly. It is replaced (and the prior
// one disposed) at the start of each new round.
let currentSession = null;

/**
 * Create a brand-new pi agent session for a round.
 *
 * A fresh SessionManager is built every call, which runs `newSession()` and so
 * gets a NEW session id AND a NEW session log file — the previous round's
 * session context (and its persisted log) is fully discarded rather than
 * resumed/continued.
 */
async function createRoundSession() {
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
    reasoning: true,
    thinkingLevelMap: {
      off: "off",
      minimal: "minimal",
      low: "low",
      medium: "medium",
      high: "high",
      xhigh: "xhigh",
      max: "max"
    },
    compat: {
      // ds4-server (not vLLM) enables thinking via the OpenAI-style
      // `reasoning_effort` field on chat/completions (it compat-maps the level
      // to the model's prefix-free effort). It does NOT accept vLLM-style
      // chat_template_kwargs, so leave thinkingFormat unset so the SDK falls
      // back to its "openai" format (params.reasoning_effort).
      supportsReasoningEffort: true
    },
    input: ["text", "image"],
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    contextWindow: 524288,
    maxTokens: 131072,
    headers: {}
  };
  // Mirror the session log to the console AND keep writing to the session file.
  const sessionManager = createConsoleMirrorSessionManager(cwd, agentDir);
  const created = await createAgentSession({ cwd, agentDir, model, sessionManager, thinkingLevel: "max" });
  const session = created.session ?? created;
  session.subscribe((event) => {
    if (event.type === "message_update") {
      const delta = event.assistantMessage?.textDelta ?? event.delta ?? "";
      if (delta) process.stdout.write(delta);
    }
  });
  return session;
}

try {
  const target = once || rounds;
  const maxRounds = target === Infinity ? Infinity : target;
  while (round <= maxRounds) {
    // Discard the previous round's session (if any) and start a fresh one.
    if (currentSession) currentSession.dispose();
    currentSession = await createRoundSession();
    console.log("pi-loop: connected to local pi agent session (" + agentDir + ")");

    const head = await runRound(currentSession, round, maxRounds);
    state = { round: round + 1, lastCommit: head, finishedAt: new Date().toISOString() };
    saveState(state);

    // The round is done — dispose this round's session so its context is
    // discarded and cannot leak into the next round.
    currentSession.dispose();
    currentSession = null;
    console.log(`pi-loop: round ${round} completed; discarding session, executing AGENTS.md loop for next round...`);
    round += 1;
    // Wait a minute after each completed loop before starting the next round.
    // Configurable via PI_LOOP_DELAY_SECONDS (default 60).
    const delaySeconds = parseInt(process.env.PI_LOOP_DELAY_SECONDS ?? "60", 10) || 0;
    if (delaySeconds > 0) {
      console.log(`pi-loop: waiting ${delaySeconds}s before round ${round}...`);
      await sleep(delaySeconds * 1000);
    }
  }
  console.log("pi-loop: finished rounds=" + (round - 1));
} catch (e) {
  console.error("pi-loop: fatal:", e.message);
  if (state) saveState(state);
  if (currentSession) currentSession.dispose();
  process.exit(1);
}

process.on("SIGINT", () => {
  console.log("\npi-loop: interrupted; persisting progress and disconnecting...");
  if (state) saveState(state);
  if (currentSession) currentSession.dispose();
  process.exit(130);
});
