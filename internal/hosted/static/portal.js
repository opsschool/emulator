// The portal: who you are, the scenarios with your best results, your
// running session, and everyone's scoreboard.

const $ = (id) => document.getElementById(id);

let me = { user: "", from_proxy: false };
let current = null; // your running session, if any

function clock(seconds) {
  seconds = Math.max(0, Math.floor(seconds));
  const m = Math.floor(seconds / 60);
  return `${m}:${String(seconds % 60).padStart(2, "0")}`;
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

async function api(path, opts = {}) {
  const r = await fetch(path, {
    ...opts,
    headers: opts.body ? { "Content-Type": "application/json" } : undefined,
  });
  if (!r.ok) throw new Error((await r.text()).trim() || r.statusText);
  return r.json();
}

function showBanner(text) {
  $("banner").textContent = text;
  $("banner").hidden = !text;
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

// ---- Tabs ----

function showTab(name) {
  for (const t of ["scenarios", "scoreboard"]) {
    $(`tab-${t}`).setAttribute("aria-selected", String(t === name));
    $(`${t}-view`).hidden = t !== name || (t === "scenarios" && !me.user);
  }
  if (name === "scoreboard") loadScoreboard();
  if (location.hash !== `#${name}`) history.replaceState(null, "", name === "scenarios" ? location.pathname : `#${name}`);
}
$("tab-scenarios").addEventListener("click", () => showTab("scenarios"));
$("tab-scoreboard").addEventListener("click", () => showTab("scoreboard"));

// ---- Who you are ----

async function loadMe() {
  me = await api("api/me");
  const named = !!me.user;
  $("name-panel").hidden = named;
  $("who").hidden = !named;
  $("who").textContent = named ? `Playing as ${me.user}` : "";
  $("rename").hidden = !named || me.from_proxy;
  return named;
}

$("rename").addEventListener("click", () => {
  $("name-panel").hidden = false;
  $("name").value = me.user;
  $("name").focus();
});

$("name-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  $("name-error").hidden = true;
  try {
    await api("api/me", { method: "POST", body: JSON.stringify({ user: $("name").value }) });
  } catch (err) {
    $("name-error").textContent = err.message;
    $("name-error").hidden = false;
    return;
  }
  await refresh();
});

// ---- Scenarios ----

const levelNames = { 1: "Level 1", 2: "Level 2", 3: "Level 3", 4: "Level 4" };

async function loadScenarios() {
  const rows = await api("api/scenarios");
  const groups = new Map();
  for (const r of rows) {
    if (!groups.has(r.category)) groups.set(r.category, []);
    groups.get(r.category).push(r);
  }
  const out = [];
  for (const [cat, list] of groups) {
    const sec = el("section", "category");
    sec.append(el("h2", null, cat));
    const grid = el("div", "grid");
    for (const r of list) grid.append(scenarioCard(r));
    sec.append(grid);
    out.push(sec);
  }
  $("scenarios").replaceChildren(...out);
}

function scenarioCard(r) {
  const card = el("article", "scenario");
  const top = el("div", "top");
  top.append(el("span", "id", r.id), el("span", "level", levelNames[r.level] || `Level ${r.level}`));
  const dl = el("dl");
  const add = (k, v, cls) => { dl.append(el("dt", null, k), el("dd", cls, v)); };
  add("Time limit", `${Math.round(r.time_limit / 60)} minutes`);
  if (r.best !== undefined) {
    add("Your best", `${r.best} points${r.hint ? " (with a hint)" : ""}`);
    if (r.fixed) add("Fastest fix", `${clock(r.fixed)} (mitigated at ${clock(r.mitigated)})`, "done");
    else if (r.mitigated) add("Mitigated", clock(r.mitigated));
  } else {
    add("Your best", "not played yet");
  }
  const start = el("button", "btn primary", r.best !== undefined ? "Play again" : "Start");
  start.disabled = !!current;
  start.title = current ? "End your running session first" : "";
  start.addEventListener("click", () => startScenario(r.id, start));
  card.append(top, dl, start);
  return card;
}

async function startScenario(id, button) {
  button.disabled = true;
  button.textContent = "Starting…";
  try {
    const s = await api("api/sessions", { method: "POST", body: JSON.stringify({ scenario: id }) });
    location.href = `s/${s.id}/`;
  } catch (e) {
    showBanner(`Couldn't start ${id}: ${e.message}`);
    button.disabled = false;
    button.textContent = "Start";
  }
}

// ---- Your session ----

async function loadCurrent() {
  current = await api("api/session");
  $("current").hidden = !current;
  if (!current) return;
  const mins = Math.max(0, Math.round((Date.now() - new Date(current.created)) / 60000));
  $("current-text").textContent = `${current.scenario}, started ${mins === 0 ? "just now" : mins === 1 ? "a minute ago" : `${mins} minutes ago`}.`;
  $("current-open").href = `s/${current.id}/`;
}

$("current-discard").addEventListener("click", () => $("discard-dialog").showModal());
$("discard-cancel").addEventListener("click", () => $("discard-dialog").close());
$("discard-confirm").addEventListener("click", async () => {
  $("discard-dialog").close();
  try {
    await api(`api/sessions/${current.id}`, { method: "DELETE" });
  } catch (e) {
    showBanner(`Couldn't discard the session: ${e.message}`);
  }
  await refresh();
});

// ---- Scoreboard ----

async function loadScoreboard() {
  let sb;
  try {
    sb = await api("api/scoreboard");
  } catch (e) {
    showBanner(`Couldn't load the scoreboard: ${e.message}`);
    return;
  }
  $("learners-empty").hidden = sb.learners.length > 0;
  $("learners").replaceChildren(...sb.learners.map((l, i) => {
    const tr = el("tr", l.user === me.user ? "me" : null);
    tr.append(el("td", "num", String(i + 1)), el("td", null, l.user), el("td", "num", String(l.points)),
      el("td", "num", String(l.fixed)), el("td", "num", String(l.played)));
    return tr;
  }));
  $("fastest").replaceChildren(...sb.scenarios.map((s) => {
    const tr = el("tr");
    const id = el("td");
    id.append(el("span", "mono", s.id));
    tr.append(id, el("td", null, s.user || "–"),
      el("td", "num", s.fixed ? clock(s.fixed) : "–"),
      el("td", "num", s.mitigated ? clock(s.mitigated) : "–"));
    return tr;
  }));
}

// ---- Start ----

async function refresh() {
  showBanner("");
  try {
    if (!(await loadMe())) {
      $("scenarios-view").hidden = true;
      $("name").focus();
      return;
    }
    if ($("tab-scenarios").getAttribute("aria-selected") === "true") $("scenarios-view").hidden = false;
    await loadCurrent();
    await loadScenarios();
  } catch (e) {
    showBanner(`Something went wrong: ${e.message}`);
  }
}

await refresh();
showTab(location.hash === "#scoreboard" ? "scoreboard" : "scenarios");
setInterval(async () => {
  if (!me.user) return;
  const had = !!current;
  try {
    await loadCurrent();
    if (had !== !!current) await loadScenarios();
  } catch (e) { /* try again next time */ }
}, 10000);
