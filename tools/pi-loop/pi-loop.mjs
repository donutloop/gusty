#!/usr/bin/env node
/**
 * pi-loop — a CLI built on the pi SDK that connects to a LOCAL pi agent
 * session and drives the AGENTS.md loop, with resume-on-stop semantics.
 *
 * Each round:
 *   1. Prompt the pi agent to read AGENTS.md and strictly follow its loop
 *      (pick the next mission feature, implement across the stack, add the
 *      machine path, add tests, update docs, run tests, commit,
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
 * Model config (no hard-coded endpoint):
 *   pi-loop does not embed a provider/model any more. It discovers a pi
 *   models.json-shaped config in the repo's setup/ dir (the SAME format the pi
 *   CLI reads from <agentDir>/models.json — see setup/pi_qwen3.8-flash-next.json),
 *   writes it verbatim to <agentDir>/models.json, and resolves the model through
 *   the SDK's own ModelRuntime so pi owns every default (baseUrl, api, compat,
 *   contextWindow, maxTokens, cost). Drop a new config under setup/ and the next
 *   round uses it — no code change.
 *   Discovery: --models-config=PATH > $PI_MODELS_CONFIG > newest setup/<named>.json
 *   (setup/pi.json is the fallback profile) > newest setup/pi*.json in the repo
 *   dir, then in tools/../setup relative to this script.
 *
 * Usage:
 *   node pi-loop.mjs [cwd] [--rounds N] [--once] [--force-reset]
 *                    [--models-config=PATH] [--provider=ID] [--model=ID]
 *                    [--dry-run] [--describe] [--help]
 * Env:
 *   PI_LOOP_SDK_PATH / PI_SDK_PATH  absolute path to the pi SDK dist/index.js
 *                    (unset is normal: the SDK is auto-discovered from the pi
 *                    managed install ~/.pi/agent/install/releases/<v>/, from the
 *                    `pi` binary on PATH, from a node dependency, or from a
 *                    global npm prefix — in that order; see sdk-discovery.mjs)
 *   PI_AGENT_DIR     agent directory (default: cwd)
 *   PI_MODELS_CONFIG path to the models config (or --models-config=)
 *   PI_SETUP_DIR     directory to discover the config in (default: <cwd>/setup)
 *   PI_PROVIDER      provider id selector when the config has several
 *   PI_MODEL         model id selector (default: first model of first provider)
 *   PI_THINKING_LEVEL   pi thinking level (default: max; clamped to the model)
 *   PI_CONTEXT_WINDOW / PI_MAX_TOKENS  numeric overrides of the config values
 *   PI_SKIP_MODEL_CHECK=1  do not probe <baseUrl>/models before round 1
 *   PI_MODEL_CHECK_TIMEOUT_MS  probe timeout (default 5000)
 *   PI_LOOP_DELAY_SECONDS      delay between rounds (default 60)
 *
 * Machine path: `--describe` prints a JSON document describing the resolved
 * config (file, provider, model, endpoint, limits, probe result) and exits —
 * for agents/scripts to check what a loop run would use without running one.
 */
import { createRequire } from "node:module";
import { spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import {
  ConfigError,
  HELP,
  availableModelIds,
  applyLimits,
  discoverModelsConfig,
  inlineModel,
  loadModelsConfigFile,
  makeSettingReader,
  modelLimits,
  probeEndpoint,
  selectModel,
  writeAgentModelsJson,
} from "./models-config.mjs";
import { resolveSdkPath } from "./sdk-discovery.mjs";

const require = createRequire(import.meta.url);

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
  const inTok = usage.input ?? usage.input_tokens ?? usage.prompt_tokens;
  const outTok = usage.output ?? usage.output_tokens ?? usage.completion_tokens;
  if (inTok != null) parts.push(`${fmtTokens(inTok)} in`);
  if (outTok != null) parts.push(`${fmtTokens(outTok)} out`);
  if (usage.reasoning != null) parts.push(`${fmtTokens(usage.reasoning)} think`);
  if (usage.cacheRead != null) parts.push(`${fmtTokens(usage.cacheRead)} cached`);
  const total = usage.totalTokens ?? usage.total_tokens;
  if (total != null) parts.push(`${fmtTokens(total)} total`);
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

function oneLine(text, limit = 200) {
  const s = String(text ?? "").replace(/\s+/g, " ").trim();
  return s.length > limit ? s.slice(0, limit) + "…" : s;
}

function shortJson(value, limit = 160) {
  if (value == null) return "";
  let s;
  try { s = JSON.stringify(value); } catch { s = String(value); }
  return s.length > limit ? s.slice(0, limit) + "…}" : s;
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
          // Tool calls and thinking blocks are rendered as compact event lines
          // instead of raw JSON blobs.
          if (c.type === "thinking" || c.thinking) return paint(`think  ${oneLine(c.thinking)}`, ansi.dim);
          if (c.type === "toolCall" || c.toolCall) return paint(`call ${c.name ?? "?"} ${shortJson(c.arguments)}`, ansi.blue);
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
// --dry-run: one throwaway round with a connectivity prompt — no state, no git
// verification, no inter-round delay. Used by the smoke test to prove the whole
// chain (config → models.json → SDK session → local endpoint) is wired up.
const dryRun = args.includes("--dry-run");
// All pi-loop settings go through this reader so PI_LOOP_<NAME> always wins and
// a parent pi session's inherited PI_MODEL / PI_PROVIDER cannot hijack the loop.
const setting = makeSettingReader(process.env);
const agentDir = setting("AGENT_DIR") || cwd;
const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const TOOL_VERSION = (() => {
  try { return JSON.parse(fs.readFileSync(path.join(scriptDir, "package.json"), "utf8")).version ?? "0.0.0"; } catch { return "0.0.0"; }
})();

const flagValue = (prefix) => {
  const hit = args.find((a) => a.startsWith(prefix));
  return hit ? hit.slice(prefix.length) : undefined;
};

const STATE_FILE = path.join(cwd, ".pi-loop-state.json");

function die(msg, code = 1) {
  console.error(`${paint("pi-loop: fatal", ansi.red)} ${msg}`);
  process.exit(code);
}

if (args.includes("--help") || args.includes("-h")) {
  console.log(HELP);
  process.exit(0);
}
const wantDescribe = args.includes("--describe") || args.includes("--json");
// ---- the pi SDK ------------------------------------------------------------
// Where the SDK lives is not knowledge pi-loop may hard-code: pi ships as a
// managed install (~/.pi/agent/install/releases/<v>/node_modules/…) that `pi
// update` rewrites, as a global npm package, or as a plain node dependency.
// sdk-discovery.mjs tries those in a documented order and, on a miss, prints
// every candidate it tried instead of a bare MODULE_NOT_FOUND stack.
let SDK;
try {
  SDK = resolveSdkPath({ cwd: path.resolve(cwd), scriptDir: path.dirname(fileURLToPath(import.meta.url)) });
} catch (e) {
  die(e instanceof ConfigError ? e.message : `pi SDK discovery failed: ${e?.stack || e}`);
}
const sdkPath = SDK.path;
const SDK_DIR = path.dirname(sdkPath);

// dist/index.js is ESM; require(ESM) works on the Node versions pi supports, but
// an SDK that ever grows top-level await would make it throw — fall back to a
// dynamic import rather than dying on a working install.
const sdkModule = await (async () => {
  try {
    return require(sdkPath);
  } catch (e) {
    if (e?.code === "ERR_REQUIRE_ESM" || e?.code === "ERR_REQUIRE_ASYNC_MODULE") {
      return import(pathToFileURL(sdkPath).href);
    }
    throw e;
  }
})();
const { createAgentSession, ModelRuntime } = sdkModule;

// The pi SDK persists every session entry to a JSONL log file under
// <agentDir>/sessions/. We keep that file persistence AND mirror the exact
// same output to the console, so operators see everything the session sees.
// SessionManager is re-exported from the SDK entry point; the direct require of
// core/session-manager.js is only a fallback for SDK builds that predate it.
const sessionModule = (() => {
  try {
    return require(path.join(SDK_DIR, "core", "session-manager.js"));
  } catch {
    return null;
  }
})();
const SessionManager = sdkModule.SessionManager ?? sessionModule?.SessionManager;
const getDefaultSessionDir = sdkModule.getDefaultSessionDir ?? sessionModule?.getDefaultSessionDir;
if (typeof SessionManager?.create !== "function" || typeof getDefaultSessionDir !== "function") {
  die(`the pi SDK at ${sdkPath} does not export SessionManager/getDefaultSessionDir; update pi or set PI_LOOP_SDK_PATH.`);
}
// Machine path: with --describe, stdout carries ONLY the JSON document; every
// human banner goes to stderr so scripts can pipe stdout straight into jq.
const printOut = console.log.bind(console);
if (wantDescribe) console.log = (...printArgs) => console.error(...printArgs);

// ---- models config: read from the repo's setup/ dir, never hard-coded -------
// The config file uses the exact schema pi reads from <agentDir>/models.json,
// so one file configures both the human-facing `pi` CLI and this agent driver.
// See setup/pi_qwen3.8-flash-next.json for the current endpoint.
let CONFIG;
try {
  const found = discoverModelsConfig({
    explicitPath: flagValue("--models-config="),
    setting,
    setupDirs: [setting("SETUP_DIR"), path.join(cwd, "setup"), path.resolve(scriptDir, "..", "..", "setup")].filter(Boolean),
  });
  const config = loadModelsConfigFile(found.path);
  const picked = selectModel(config, {
    file: found.path,
    provider: flagValue("--provider=") ?? setting("PROVIDER"),
    model: flagValue("--model=") ?? setting("MODEL"),
  });
  CONFIG = { ...found, config, ...picked };
} catch (e) {
  die(e instanceof ConfigError ? e.message : `models config error: ${e?.stack || e}`);
}

const MODELS_JSON = path.join(agentDir, "models.json");
const { providerId, modelId, providerCfg, modelCfg } = CONFIG;
const thinkingLevel = setting("THINKING_LEVEL") || "max";

// Publish the config to <agentDir>/models.json before creating any session.
const written = writeAgentModelsJson(MODELS_JSON, CONFIG.config);
if (written.droppedLegacy) {
  console.log(`pi-loop: dropped the legacy top-level "models" key from ${MODELS_JSON} (pi rejects the file outright with it present)`);
}

// Ask the endpoint what it actually serves: fail fast rather than after a round,
// and pick up max_model_len when the config leaves contextWindow out.
const endpoint = setting("SKIP_MODEL_CHECK") === "1"
  ? { skipped: true }
  : await probeEndpoint({
      baseUrl: providerCfg.baseUrl,
      apiKey: providerCfg.apiKey,
      modelId,
      timeoutMs: Number(setting("MODEL_CHECK_TIMEOUT_MS")) || 5000,
    });

const limits = modelLimits({ modelCfg, setting, serverMaxModelLen: endpoint.serverMaxModelLen ?? null });

const rel = (p) => {
  const r = path.relative(cwd, p);
  return r && !r.startsWith("..") ? r : p;
};
const where = CONFIG.source === "auto-discovered" ? `auto-discovered in ${rel(CONFIG.dir)}` : CONFIG.source;

const ignoredSettings = setting.ignored();
if (ignoredSettings.length) {
  console.log(
    `pi-loop: ignoring ${ignoredSettings.join(", ")} inherited from the parent pi session — they name that session's ` +
    `model, not the loop's (use --model= / PI_LOOP_MODEL to choose one)`
  );
}

// Resolve the model through the SDK's own ModelRuntime: pi fills in every
// default (baseUrl, api, compat, cost) from <agentDir>/models.json, so pi-loop
// never carries a second copy of the endpoint definition.
const MODEL_RUNTIME = await ModelRuntime.create({ modelsPath: MODELS_JSON, allowModelNetwork: false });
const runtimeError = MODEL_RUNTIME.getError();
if (runtimeError) console.warn(`pi-loop: ${runtimeError}`);

const resolvedModel = MODEL_RUNTIME.getPhysicalModel?.(providerId, modelId) ?? MODEL_RUNTIME.getModel?.(providerId, modelId);
// Keep whatever pi resolved (its defaults are conservative: 128k context / 16k
// output); only stated numbers — PI_LOOP_*, the config, or the endpoint's own
// max_model_len — replace them.
const SESSION_MODEL = resolvedModel
  ? applyLimits(resolvedModel, limits)
  : inlineModel({ providerId, modelId, providerCfg, modelCfg, contextWindow: limits.contextWindow, maxTokens: limits.maxTokens });
if (!resolvedModel) {
  console.warn(`pi-loop: SDK could not resolve ${providerId}/${modelId} from ${MODELS_JSON}; using an inline model definition.`);
}

console.log(
  `pi-loop: models config ${rel(CONFIG.path)} (${where}) → ` +
  `${paint(`${providerId}/${modelId}`, ansi.green)} @ ${SESSION_MODEL.baseUrl} ` +
  `(context ${SESSION_MODEL.contextWindow}${limits.contextWindowFrom === "endpoint" ? " = endpoint max_model_len" : ""}, ` +
  `maxTokens ${SESSION_MODEL.maxTokens}, thinking ${thinkingLevel}${SESSION_MODEL.reasoning ? "" : " (server default effort: pi sends no reasoning_effort)"})`
);

if (endpoint.skipped) {
  console.log("pi-loop: endpoint check skipped (PI_SKIP_MODEL_CHECK=1)");
} else if (!endpoint.reachable) {
  die(
    `endpoint ${providerCfg.baseUrl} is not reachable (${endpoint.error}). ` +
    `Start the local model server (see setup/boot_agent.sh) or set PI_SKIP_MODEL_CHECK=1 to try anyway.`
  );
} else if (!endpoint.modelListed) {
  die(
    `${providerCfg.baseUrl} does not serve "${modelId}" (it serves: ${endpoint.served.join(", ") || "nothing"}). ` +
    `Pick one with --model=ID / PI_LOOP_MODEL, or update the config in setup/.`
  );
}

// Machine path: let an agent/script see exactly what a run would use.
if (wantDescribe) {
  printOut(JSON.stringify({
    ok: true,
    tool: "pi-loop",
    version: TOOL_VERSION,
    sdk: { path: sdkPath, source: SDK.source, packageDir: SDK.packageDir, version: SDK.version },
    cwd: path.resolve(cwd),
    agentDir: path.resolve(agentDir),
    modelsConfig: path.resolve(CONFIG.path),
    modelsConfigSource: CONFIG.source,
    modelsJson: path.resolve(MODELS_JSON),
    provider: providerId,
    model: modelId,
    baseUrl: SESSION_MODEL.baseUrl,
    api: SESSION_MODEL.api,
    contextWindow: SESSION_MODEL.contextWindow,
    maxTokens: SESSION_MODEL.maxTokens,
    limits: {
      contextWindow: { value: limits.contextWindow, from: limits.contextWindowFrom },
      maxTokens: { value: limits.maxTokens, from: limits.maxTokensFrom },
    },
    reasoning: SESSION_MODEL.reasoning ?? false,
    // "off" here means pi sends no reasoning_effort and the server applies its
    // own default effort — it does NOT mean the model answers without thinking.
    thinkingLevel,
    thinkingControlledByPi: !!SESSION_MODEL.reasoning,
    input: SESSION_MODEL.input ?? ["text"],
    compat: SESSION_MODEL.compat ?? null,
    availableModels: availableModelIds(CONFIG.config),
    ignoredInheritedSettings: ignoredSettings,
    endpoint,
  }, null, 2));
  process.exit(0);
}

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
  const prompt = dryRun
    ? "Connectivity check only — reply with exactly PI_LOOP_OK. Do not read, edit or run anything."
    : [
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
  if (!clean && !dryRun) {
    console.warn(`pi-loop: round ${round} left an uncommitted working tree.`);
  }
  return head;
}

// Decide resume vs re-execute AGENTS.md.
let state = loadState();
let round = 1;
if (dryRun) {
  console.log("pi-loop: DRY RUN — one connectivity round, progress state is not touched");
} else if (!forceReset && state) {
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
  // The model object is resolved once at startup from <agentDir>/models.json
  // (written from the setup/ models config) by the SDK's own ModelRuntime, and
  // that same runtime is handed to createAgentSession so auth, compat and the
  // thinking clamp stay exactly as pi configures them for the CLI too.
  const sessionManager = createConsoleMirrorSessionManager(cwd, agentDir);
  const created = await createAgentSession({
    cwd,
    agentDir,
    model: SESSION_MODEL,
    modelRuntime: MODEL_RUNTIME,
    sessionManager,
    thinkingLevel,
    // A dry run must never be able to touch the repo: no tools at all.
    ...(dryRun ? { noTools: "all" } : {}),
  });
  const session = created.session ?? created;
  session.subscribe((event) => {
    if (event.type !== "message_update") return;
    // pi streams via assistantMessageEvent ({ type: "text_delta", delta }).
    const ev = event.assistantMessageEvent;
    const delta = ev?.type === "text_delta" ? ev.delta : (event.assistantMessage?.textDelta ?? event.delta ?? "");
    if (delta) process.stdout.write(paint(delta, ansi.dim));
  });
  return session;
}

try {
  const target = dryRun ? 1 : once || rounds;
  const maxRounds = target === Infinity ? Infinity : target;
  while (round <= maxRounds) {
    // Discard the previous round's session (if any) and start a fresh one.
    if (currentSession) currentSession.dispose();
    currentSession = await createRoundSession();
    console.log(`pi-loop: connected to local pi agent session (${agentDir}) on ${providerId}/${modelId}`);

    const head = await runRound(currentSession, round, maxRounds);
    if (!dryRun) {
      state = { round: round + 1, lastCommit: head, finishedAt: new Date().toISOString() };
      saveState(state);
    }

    // The round is done — dispose this round's session so its context is
    // discarded and cannot leak into the next round.
    currentSession.dispose();
    currentSession = null;
    console.log(`pi-loop: round ${round} completed; discarding session, executing AGENTS.md loop for next round...`);
    round += 1;
    if (dryRun) continue;
    // Wait a minute after each completed loop before starting the next round.
    // Configurable via PI_LOOP_DELAY_SECONDS (default 60).
    const delaySeconds = parseInt(setting("DELAY_SECONDS") ?? "60", 10) || 0;
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
