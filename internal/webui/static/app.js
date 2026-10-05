// The session page: polls the daemon's /status, runs terminals over
// /api/terminal, draws /api/charts, and calls /hint, /verify and /stop.
// Every URL is relative: the hosted portal serves the page under
// /s/<session>/ and passes those calls on to the session.
import { Terminal } from "./vendor/xterm.mjs";
import { FitAddon } from "./vendor/addon-fit.mjs";

const $ = (id) => document.getElementById(id);
const NS = 1e9; // Go durations arrive as nanoseconds

let status = null; // last /status reply
let statusAt = 0; // when it arrived, to run the clock between polls
let ended = false;
let view = "terminal";
let hintConfirming = false;
let hosted = false; // served by the hosted portal
let ready = false; // the session has answered once

// ---- Formatting ----

function clock(seconds) {
  seconds = Math.max(0, Math.floor(seconds));
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = String(seconds % 60).padStart(2, "0");
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${s}` : `${m}:${s}`;
}

function sinceStart(time) {
  const st = status.state;
  return (new Date(time) - new Date(st.started_at)) / 1000;
}

// ---- Theme ----

$("theme").addEventListener("click", () => {
  const root = document.documentElement;
  const dark = root.dataset.theme
    ? root.dataset.theme === "dark"
    : matchMedia("(prefers-color-scheme: dark)").matches;
  const next = dark ? "light" : "dark";
  root.dataset.theme = next;
  try { localStorage.setItem("theme", next); } catch (e) { /* private window */ }
});

// ---- Status ----

async function poll() {
  if (ended) return;
  try {
    const r = await fetch("status");
    // While a hosted session starts, it answers with how setup is going.
    if (r.status === 503 && (r.headers.get("content-type") || "").includes("json")) {
      hosted = true;
      showStarting(await r.json());
      return;
    }
    if (!r.ok) throw new Error(await r.text());
    status = await r.json();
    statusAt = Date.now();
    $("banner").hidden = true;
    $("starting").hidden = true;
    render();
    if (!ready) {
      ready = true;
      // The terminal measures its font, so wait for it.
      document.fonts.load('14px "Atkinson Hyperlegible Mono"').finally(() => newShell());
    }
  } catch (e) {
    showBanner(hosted
      ? "Can't reach the session. If it has ended, you can go back to the scenarios."
      : "Can't reach the session. If it has ended you can close this tab; otherwise check that `opsschool start` finished without errors.");
  }
}

function showStarting(s) {
  showHome("/");
  $("waiting").hidden = true;
  $("paged").hidden = true;
  $("starting").hidden = false;
  $("starting-msg").textContent = s.error || s.message || "";
  $("starting-msg").classList.toggle("bad", !!s.error);
  $("starting-bar").hidden = !!s.error;
  $("starting-help").hidden = !!s.error;
  $("clock").textContent = s.error ? "" : "Setting up…";
  if (s.error) ended = true;
}

function showHome(url) {
  $("home").href = url;
  $("home").hidden = false;
}

function showBanner(text) {
  $("banner").textContent = text;
  $("banner").hidden = false;
}

function begun() {
  return status && !status.state.started_at.startsWith("0001-");
}

function elapsed() {
  return status.elapsed / NS + (Date.now() - statusAt) / 1000;
}

function overTime() {
  const limit = status.state.time_limit / NS;
  return begun() && limit > 0 && elapsed() > limit;
}

function tickClock() {
  if (!status || ended) return;
  const st = status.state;
  let text = "";
  if (begun()) {
    const limit = st.time_limit / NS;
    text = `${clock(elapsed())} elapsed`;
    if (limit > 0) text += ` · ${clock(limit)} limit`;
    if (overTime()) text = "Time's up";
  } else if (st.baseline_ends) {
    const left = (new Date(st.baseline_ends) - Date.now()) / 1000;
    text = left > 0 ? `Scenario begins in ${clock(left)}` : "Scenario is starting…";
    $("waiting-time").textContent = text;
  } else {
    text = "Getting ready…";
  }
  $("clock").textContent = text;
}

function render() {
  const st = status.state;
  const page = status.page;
  const isBegun = begun();
  document.title = `${page.id} · Ops School`;
  $("scenario-id").textContent = page.id;
  $("grafana").href = page.grafana_url;
  if (page.home_url) {
    hosted = true;
    showHome(page.home_url);
  }

  $("waiting").hidden = isBegun;
  $("paged").hidden = !isBegun;
  $("summary").textContent = page.summary || "";
  // These are the alerts that paged you, not live alert state.
  const recovered = !!(st.tier_passed || {}).mitigated;
  $("alerts").replaceChildren(...(page.alerts || []).map((a) => {
    const div = document.createElement("div");
    div.className = recovered ? "alert recovered" : "alert";
    const strong = document.createElement("strong");
    strong.textContent = "Paged";
    div.append(strong, " · " + a);
    return div;
  }));

  // Progress.
  const passed = st.tier_passed || {};
  for (const tier of ["mitigated", "fixed"]) {
    $(`tier-${tier}`).classList.toggle("passed", !!passed[tier]);
    $(`dot-${tier}`).classList.toggle("passed", !!passed[tier]);
    $(`${tier}-at`).textContent = passed[tier] ? `at ${clock(sinceStart(passed[tier]))}` : "";
  }
  $("tier-fixed").textContent = passed.fixed ? "Fixed" : "Fixed: not yet";
  $("tier-mitigated").textContent = passed.mitigated ? "Mitigated" : "Mitigated: not yet";

  const holding = !passed.mitigated && status.hold_left > 0;
  $("dot-mitigated").classList.toggle("active", holding);
  if (passed.mitigated) {
    $("mitigated-text").textContent = "Customers are being served again. Nice work.";
  } else if (holding) {
    $("mitigated-text").textContent = `Looking healthy. Keep it that way for ${clock(status.hold_left / NS)} more.`;
  } else {
    $("mitigated-text").textContent = "Get customers checking out again. We check this all the time.";
  }

  const lv = st.last_verify;
  if (passed.fixed) {
    $("fixed-text").textContent = "Verified. Your fix held through a restart and a rush of customers.";
  } else if (status.verifying) {
    $("fixed-text").textContent = "Checking your fix now…";
  } else if (lv && !lv.pass) {
    $("fixed-text").textContent = "The last check didn't pass. These still need work:";
  } else {
    $("fixed-text").textContent = "When you think the cause is gone, press Verify. We restart the shop and replay a rush of customers. You can verify as often as you like.";
  }
  const showFailures = !passed.fixed && !status.verifying && lv && !lv.pass && (lv.failures || []).length > 0;
  $("failures").hidden = !showFailures;
  if (showFailures) {
    $("failures").replaceChildren(...lv.failures.map((f) => {
      const li = document.createElement("li");
      li.textContent = f;
      return li;
    }));
  }
  if (st.data_loss) {
    $("fixed-text").textContent = "Some orders were lost during a check, so the fixed tier can't pass in this session.";
  }

  const verify = $("verify");
  verify.disabled = !isBegun || status.verifying || !!passed.fixed || st.data_loss || overTime();
  verify.textContent = status.verifying ? "Verifying…" : "Verify my fix";

  renderHints(page, isBegun);
}

function renderHints(page, isBegun) {
  $("docs-card").hidden = !page.has_docs;
  $("docs-show").hidden = !!page.docs;
  $("docs-show").disabled = !isBegun;
  const link = $("docs-link");
  link.hidden = !page.docs;
  if (page.docs) {
    link.href = page.docs;
    link.textContent = page.docs.replace(/^https?:\/\/(www\.)?/, "");
  }

  $("hint-card").hidden = page.hints_total === 0;
  const shown = page.hints || [];
  $("hint-text").hidden = shown.length === 0;
  $("hint-text").textContent = shown.join("\n\n");
  const done = shown.length >= page.hints_total;
  const needDocsFirst = page.has_docs && !page.docs;
  if (done) {
    hintConfirming = false;
    $("hint-help").textContent = "Here's your hint.";
  } else if (needDocsFirst) {
    $("hint-help").textContent = "Have a look at the free hint first. If you're still stuck, come back here.";
  } else {
    $("hint-help").textContent = "It points you in the right direction without giving the answer away. We all need one sometimes.";
  }
  $("hint-show").hidden = done || hintConfirming;
  $("hint-show").disabled = !isBegun || needDocsFirst;
  $("hint-confirm").hidden = done || !hintConfirming;
  $("hint-cancel").hidden = done || !hintConfirming;
}

async function post(path) {
  const r = await fetch(path, { method: "POST" });
  if (!r.ok) throw new Error((await r.text()).trim());
  return r.json();
}

$("docs-show").addEventListener("click", async () => {
  try { await post("hint"); } catch (e) { showBanner(e.message); }
  poll();
});
$("hint-show").addEventListener("click", () => { hintConfirming = true; render(); $("hint-confirm").focus(); });
$("hint-cancel").addEventListener("click", () => { hintConfirming = false; render(); });
$("hint-confirm").addEventListener("click", async () => {
  hintConfirming = false;
  try { await post("hint"); } catch (e) { showBanner(e.message); }
  poll();
});

// ---- Verify ----

let verifying = false;
$("verify-dialog").addEventListener("cancel", (e) => { if (verifying) e.preventDefault(); });
$("verify-close").addEventListener("click", () => $("verify-dialog").close());

$("verify").addEventListener("click", async () => {
  const log = $("verify-log");
  const result = $("verify-result");
  log.replaceChildren();
  result.replaceChildren();
  $("verify-close").disabled = true;
  $("verify-dialog").showModal();
  verifying = true;
  const line = (text) => {
    const li = document.createElement("li");
    li.textContent = text;
    log.append(li);
    log.scrollTop = log.scrollHeight;
  };
  try {
    const r = await fetch("verify", { method: "POST" });
    if (!r.ok) throw new Error((await r.text()).trim());
    const reader = r.body.pipeThrough(new TextDecoderStream()).getReader();
    let buf = "";
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += value;
      let i;
      while ((i = buf.indexOf("\n")) >= 0) {
        const text = buf.slice(0, i);
        buf = buf.slice(i + 1);
        if (text.startsWith("RESULT ")) showVerifyResult(JSON.parse(text.slice(7)));
        else if (text.startsWith("ERROR ")) showVerifyError(text.slice(6));
        else if (text) line(text);
      }
    }
  } catch (e) {
    showVerifyError(e.message);
  }
  verifying = false;
  $("verify-close").disabled = false;
  $("verify-close").focus();
  poll();
});

function showVerifyResult(rep) {
  const box = document.createElement("div");
  if (rep.pass) {
    box.className = "result-ok";
    box.textContent = "Fixed! Your fix held through a restart and a rush of customers.";
  } else {
    box.className = "result-bad";
    box.textContent = rep.data_loss
      ? "Some orders were lost during the check, so the fixed tier can't pass in this session."
      : "Not fixed yet. These checks didn't pass:";
    const ul = document.createElement("ul");
    for (const f of rep.failures || []) {
      const li = document.createElement("li");
      li.textContent = f;
      ul.append(li);
    }
    box.append(ul);
  }
  $("verify-result").replaceChildren(box);
}

function showVerifyError(text) {
  const box = document.createElement("div");
  box.className = "result-bad";
  box.textContent = `The check couldn't run: ${text}`;
  $("verify-result").replaceChildren(box);
}

// ---- End session ----

$("end").addEventListener("click", () => {
  $("end-ask").hidden = false;
  $("end-done").hidden = true;
  $("end-dialog").showModal();
});
$("end-cancel").addEventListener("click", () => $("end-dialog").close());
$("end-confirm").addEventListener("click", async () => {
  $("end-confirm").disabled = true;
  let res;
  try {
    res = await post("stop?teardown=1");
  } catch (e) {
    $("end-confirm").disabled = false;
    showBanner(`Couldn't end the session: ${e.message}`);
    $("end-dialog").close();
    return;
  }
  ended = true;
  for (const sh of shells) sh.ws?.close();
  const out = $("end-result");
  out.replaceChildren();
  const score = document.createElement("div");
  score.className = "score";
  score.textContent = `${res.score} points`;
  out.append(score);
  for (const tier of ["mitigated", "fixed"]) {
    const p = document.createElement("p");
    const at = res.tier_passed?.[tier];
    p.textContent = at !== undefined ? `${tier[0].toUpperCase() + tier.slice(1)}: passed at ${clock(at)}` : `${tier[0].toUpperCase() + tier.slice(1)}: not passed`;
    out.append(p);
  }
  const hint = document.createElement("p");
  hint.textContent = res.hints_used > 0 ? "Hint: used" : "Hint: not used";
  out.append(hint);
  $("end-ask").hidden = true;
  $("end-done").hidden = false;
  for (const id of ["verify", "end", "docs-show", "hint-show"]) $(id).disabled = true;
  $("end-gone").hidden = hosted;
  $("end-home").hidden = !hosted;
  showBanner("This session has ended.");
});

// ---- Workspace tabs ----

function showView(next) {
  view = next;
  for (const v of ["terminal", "dashboard"]) {
    $(`tab-${v}`).setAttribute("aria-selected", String(v === next));
    $(`${v}-view`).hidden = v !== next;
  }
  $("term-tips").hidden = next !== "terminal";
  if (next === "terminal" && active) {
    active.fit.fit();
    active.term.focus();
  }
  if (next === "dashboard") loadCharts();
}
$("tab-terminal").addEventListener("click", () => showView("terminal"));
$("tab-dashboard").addEventListener("click", () => showView("dashboard"));

// ---- Terminals ----

const termTheme = {
  background: "#0D1117", foreground: "#D6DEE8", cursor: "#D6DEE8", cursorAccent: "#0D1117",
  selectionBackground: "#3B4A5E",
  black: "#0D1117", red: "#FF7B72", green: "#7EE2A8", yellow: "#E3B341",
  blue: "#79C0FF", magenta: "#D2A8FF", cyan: "#76E3EA", white: "#D6DEE8",
  brightBlack: "#8B96A5", brightRed: "#FFA198", brightGreen: "#A6F0C6", brightYellow: "#F2CC60",
  brightBlue: "#A5D6FF", brightMagenta: "#E2C5FF", brightCyan: "#B3F0FF", brightWhite: "#FFFFFF",
};

const shells = [];
let active = null;
let shellCount = 0;
const encoder = new TextEncoder();

function newShell() {
  const n = ++shellCount;
  const pane = document.createElement("div");
  pane.className = "term-pane";
  $("term-body").append(pane);
  const term = new Terminal({
    fontFamily: '"Atkinson Hyperlegible Mono", ui-monospace, monospace',
    fontSize: 14,
    lineHeight: 1.25,
    theme: termTheme,
    cursorBlink: true,
    scrollback: 5000,
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.open(pane);

  const tab = document.createElement("button");
  tab.className = "term-tab";
  tab.setAttribute("role", "tab");
  const label = document.createElement("span");
  label.textContent = `Shell ${n}`;
  const close = document.createElement("span");
  close.className = "close";
  close.setAttribute("role", "button");
  close.setAttribute("aria-label", `Close shell ${n}`);
  close.textContent = "×";
  tab.append(label, close);
  $("term-tablist").append(tab);

  const sh = { n, pane, term, fit, tab, ws: null, state: "connecting" };
  tab.addEventListener("click", (e) => {
    if (e.target === close) closeShell(sh);
    else select(sh);
  });
  const send = (data) => {
    if (sh.ws && sh.ws.readyState === WebSocket.OPEN) sh.ws.send(data);
    else if (sh.state === "closed" && !ended) connect(sh);
  };
  // Copy on select, as Linux terminals do. Ctrl+Shift+C copies too; the
  // browser would otherwise open its inspector. Ctrl+Shift+V pastes as it is.
  term.onSelectionChange(() => {
    const text = term.getSelection();
    if (text) copy(text);
  });
  term.attachCustomKeyEventHandler((e) => {
    if (e.ctrlKey && e.shiftKey && e.code === "KeyC") {
      if (e.type === "keydown" && term.hasSelection()) copy(term.getSelection());
      e.preventDefault();
      return false;
    }
    return true;
  });
  // The middle button pastes the last selection. Chrome on Linux has its
  // own middle-click paste, so stop that to avoid pasting twice.
  for (const type of ["mousedown", "mouseup", "auxclick"]) {
    pane.addEventListener(type, (e) => {
      if (e.button !== 1) return;
      e.preventDefault();
      e.stopPropagation();
      if (type === "mouseup" && selection) term.paste(selection);
    }, true);
  }
  term.onData((d) => send(encoder.encode(d)));
  term.onBinary((d) => send(Uint8Array.from(d, (c) => c.charCodeAt(0))));
  term.onResize(() => sendSize(sh));

  shells.push(sh);
  updateTabs();
  select(sh);
  connect(sh);
}

function connect(sh) {
  sh.state = "connecting";
  updateState();
  const url = new URL("api/terminal", location.href);
  url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(url);
  ws.binaryType = "arraybuffer";
  sh.ws = ws;
  ws.onopen = () => {
    sh.state = "connected";
    sendSize(sh);
    updateState();
  };
  ws.onmessage = (e) => sh.term.write(new Uint8Array(e.data));
  ws.onclose = () => {
    if (sh.ws !== ws) return;
    sh.ws = null;
    sh.state = "closed";
    if (!ended) sh.term.write("\r\n\x1b[33mDisconnected. Press any key to reconnect.\x1b[0m\r\n");
    updateState();
  };
}

function sendSize(sh) {
  if (sh.ws && sh.ws.readyState === WebSocket.OPEN) {
    sh.ws.send(JSON.stringify({ type: "resize", cols: sh.term.cols, rows: sh.term.rows }));
  }
}

function select(sh) {
  active = sh;
  for (const s of shells) {
    s.pane.hidden = s !== sh;
    s.tab.setAttribute("aria-selected", String(s === sh));
  }
  if (view === "terminal") {
    sh.fit.fit();
    sh.term.focus();
  }
  updateState();
}

function closeShell(sh) {
  const ws = sh.ws;
  sh.ws = null;
  ws?.close();
  sh.term.dispose();
  sh.pane.remove();
  sh.tab.remove();
  shells.splice(shells.indexOf(sh), 1);
  if (shells.length === 0) newShell();
  else if (active === sh) select(shells[shells.length - 1]);
  updateTabs();
}

function updateTabs() {
  for (const s of shells) s.tab.querySelector(".close").hidden = shells.length === 1;
}

function updateState() {
  const el = $("term-state");
  const state = active ? active.state : "connecting";
  el.className = `term-state ${state}`;
  el.textContent = { connecting: "connecting…", connected: "connected", closed: "disconnected" }[state];
}

// The last text selected in any shell, for middle-click paste.
let selection = "";
function copy(text) {
  selection = text;
  navigator.clipboard?.writeText(text).catch(() => {});
}

// Browsers keep shortcuts like Ctrl+W (close tab) for themselves. In full
// screen, Chrome and Edge let a page take them with the Keyboard Lock API.
$("term-full").addEventListener("click", async () => {
  if (document.fullscreenElement) return document.exitFullscreen();
  try {
    await $("terminal-view").requestFullscreen();
    await navigator.keyboard?.lock?.();
  } catch (e) {
    /* full screen without the lock still helps */
  }
});
document.addEventListener("fullscreenchange", () => {
  const on = document.fullscreenElement === $("terminal-view");
  if (!on) navigator.keyboard?.unlock?.();
  $("term-full").textContent = on ? "Exit full screen" : "Full screen";
  if (active) {
    active.fit.fit();
    active.term.focus();
  }
});

// A stray Ctrl+W outside full screen would close the tab and its shells,
// so ask first.
window.addEventListener("beforeunload", (e) => {
  if (!ended && shells.some((s) => s.state === "connected")) {
    e.preventDefault();
    e.returnValue = "";
  }
});

$("term-add").addEventListener("click", () => newShell());
new ResizeObserver(() => {
  if (active && view === "terminal") active.fit.fit();
}).observe($("term-body"));

// ---- Charts ----

const SVG = "http://www.w3.org/2000/svg";
const colors = ["var(--chart-1)", "var(--chart-2)", "var(--chart-3)", "var(--chart-4)"];

function fmt(v, unit) {
  if (v === undefined || v === null) return "–";
  if (unit === "percent") return `${v < 10 ? v.toFixed(1) : Math.round(v)}%`;
  if (unit === "seconds") return v < 1 ? `${Math.round(v * 1000)} ms` : `${v.toFixed(2)} s`;
  return String(Math.round(v));
}

// ticks picks round numbers for the vertical axis, from 0 to just above max.
function ticks(max, unit) {
  const floor = unit === "percent" ? 5 : unit === "seconds" ? 0.1 : 10;
  let want = Math.max(floor, max * 1.1);
  if (unit === "percent") want = Math.min(100, want);
  const rough = want / 5;
  const pow = 10 ** Math.floor(Math.log10(rough));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * pow).find((s) => s >= rough);
  const top = Math.ceil(want / step - 1e-9) * step;
  const out = [];
  for (let v = 0; v <= top + step / 1000; v += step) out.push(+v.toPrecision(6));
  return out;
}

function tickLabel(v, unit) {
  if (unit === "percent") return `${+v.toFixed(1)}%`;
  if (unit === "seconds") return v === 0 ? "0" : v < 1 ? `${Math.round(v * 1000)} ms` : `${+v.toFixed(2)} s`;
  return String(+v.toFixed(1));
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function svgEl(tag, attrs) {
  const e = document.createElementNS(SVG, tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, String(v));
  return e;
}

async function loadCharts() {
  try {
    const r = await fetch("api/charts?minutes=15");
    if (!r.ok) throw new Error(await r.text());
    drawCharts((await r.json()).charts);
  } catch (e) {
    /* the next refresh tries again */
  }
}

function drawCharts(charts) {
  const box = $("charts");
  box.replaceChildren(...charts.map(drawChart));
}

function drawChart(c) {
  const card = document.createElement("div");
  card.className = "chart";
  const head = document.createElement("div");
  head.className = "chart-head";
  const title = document.createElement("strong");
  title.textContent = c.title;
  const value = document.createElement("span");
  value.className = "chart-value";
  head.append(title, value);
  const help = document.createElement("div");
  help.className = "muted small";
  help.textContent = c.help;
  card.append(head, help);

  const series = (c.series || []).filter((s) => s.points && s.points.length > 0);
  const minutes = 15;
  const now = Date.now() / 1000;
  const t0 = now - minutes * 60;
  const all = series.flatMap((s) => s.points.map((p) => p[1]));
  const yt = ticks(all.length ? Math.max(...all) : 0, c.unit);
  const ymax = yt[yt.length - 1];

  // The lines are drawn in a stretched SVG; the labels are HTML placed by
  // percentage, so text keeps its shape at any width.
  const W = 600, H = 140;
  const x = (t) => ((t - t0) / (now - t0)) * W;
  const y = (v) => H - (Math.min(v, ymax) / ymax) * H;
  const svg = svgEl("svg", { viewBox: `0 0 ${W} ${H}`, preserveAspectRatio: "none", role: "img", "aria-label": c.title });
  const yLabels = el("div", "y-ticks");
  for (const v of yt) {
    svg.append(svgEl("line", {
      class: v === 0 ? "axis" : "grid", x1: 0, x2: W, y1: y(v), y2: y(v), "vector-effect": "non-scaling-stroke",
    }));
    const l = el("span", "", tickLabel(v, c.unit));
    l.style.top = `${(y(v) / H) * 100}%`;
    yLabels.append(l);
  }
  const xLabels = el("div", "x-ticks");
  for (let m = minutes; m >= 0; m -= 5) {
    const l = el("span", "", m === 0 ? "now" : String(m));
    l.style.left = `${((minutes - m) / minutes) * 100}%`;
    xLabels.append(l);
  }

  if (series.length === 0) {
    value.textContent = "–";
    const t = el("div", "empty", c.error || "No data yet");
    card.append(plot(c, yLabels, svg, xLabels, t));
    return card;
  }

  series.forEach((s, i) => {
    const line = svgEl("polyline", {
      points: s.points.map((p) => `${x(p[0]).toFixed(1)},${y(p[1]).toFixed(1)}`).join(" "),
      fill: "none", "stroke-width": 2, "vector-effect": "non-scaling-stroke",
    });
    line.style.stroke = colors[i % colors.length];
    svg.append(line);
  });
  card.append(plot(c, yLabels, svg, xLabels));

  const last = (s) => s.points[s.points.length - 1][1];
  if (series.length === 1) {
    value.textContent = fmt(last(series[0]), c.unit);
  } else {
    value.textContent = `${fmt(Math.max(...series.map(last)), c.unit)} highest`;
    const legend = el("div", "legend");
    series.forEach((s, i) => {
      const span = el("span", "", `${s.label || "series " + (i + 1)} ${fmt(last(s), c.unit)}`);
      span.style.setProperty("--swatch", colors[i % colors.length]);
      legend.append(span);
    });
    card.append(legend);
  }
  return card;
}

// plot lays out the axis titles, tick labels and lines of one chart.
function plot(c, yLabels, svg, xLabels, overlay) {
  const box = el("div", "plot");
  const area = el("div", "plot-area");
  area.append(svg);
  if (overlay) area.append(overlay);
  box.append(
    el("div", "y-title", c.axis),
    yLabels, area,
    el("div"), xLabels,
    el("div"), el("div", "x-title", "minutes ago"),
  );
  return box;
}

setInterval(() => { if (view === "dashboard" && !ended) loadCharts(); }, 15000);

// ---- Start ----

// The first terminal opens once the session answers; see poll.
poll();
setInterval(poll, 2000);
setInterval(tickClock, 1000);
