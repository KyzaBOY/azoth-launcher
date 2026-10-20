// Screen logic: the account panel (Discord), Play with its single status line,
// and the settings dialog (the New World folder, with Browse and Search).
(function () {
  "use strict";
  const { api, bus, win, real, params } = window.AzothBridge;
  const $ = (id) => document.getElementById(id);

  const state = { cfg: null, account: null, busy: false, loggingIn: false, searching: false, found: [], selected: "" };

  // ---------- helpers ----------
  const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
  const errText = (e) => { const m = (e && e.message) ? e.message : String(e); return m.charAt(0).toUpperCase() + m.slice(1); };

  let toastTimer = 0;
  function toast(msg, ok) {
    const t = $("toast");
    t.textContent = msg; t.classList.toggle("is-ok", !!ok); t.hidden = false;
    clearTimeout(toastTimer); toastTimer = setTimeout(() => (t.hidden = true), ok ? 3500 : 7000);
  }

  // The one status line under Play: progress and errors.
  function setStatus(text, isError) {
    const p = $("play-status");
    p.textContent = text || "";
    p.classList.toggle("is-error", !!isError);
  }

  // ---------- Play ----------
  function updatePlay() {
    const logged = !!(state.account && state.account.loggedIn);
    const btn = $("btn-play");
    btn.disabled = !logged || state.busy;
    btn.classList.toggle("is-busy", state.busy && !state.loggingIn);
    btn.querySelector(".btn-play-text").textContent = state.busy && !state.loggingIn ? "Launching" : "Play";
    btn.title = logged ? "" : "Log in with Discord to play";
  }

  function setBusy(b) {
    state.busy = !!b;
    updatePlay();
    renderAccountButtons();
  }

  async function launch() {
    if (state.busy) return;
    if (!state.account || !state.account.loggedIn) return;
    if (!state.cfg || !state.cfg.gameDir) {
      setStatus("Choose your New World folder in Settings first.", true);
      openSettings();
      return;
    }
    setBusy(true);
    setStatus("Checking game files…");
    try {
      const r = await api.Launch();
      if (r.ok) setStatus("Game started");
      else setStatus(r.error || "Something went wrong.", true);
    } catch (e) {
      setStatus(errText(e), true);
    } finally {
      setBusy(false);
    }
  }

  // ---------- account (Discord login) ----------
  function showAccountError(msg) { const p = $("account-err"); p.textContent = msg || ""; p.hidden = !msg; }
  function renderAccount(a) {
    state.account = a;
    const on = !!(a && a.loggedIn);
    $("account-out").hidden = on; $("account-in").hidden = !on;
    if (on) {
      const name = a.name || a.id || "?";
      $("account-name").textContent = name;
      $("account-name").title = name;
      $("avatar").textContent = Array.from(name)[0].toUpperCase();
      showAccountError("");
    }
    renderAccountButtons();
    updatePlay();
  }
  function renderAccountButtons() {
    $("btn-login").disabled = state.loggingIn || state.busy;
    $("btn-logout").disabled = state.busy;
    $("account-note").hidden = state.loggingIn;
    $("account-wait").hidden = !state.loggingIn;
  }
  async function loginDiscord() {
    if (state.loggingIn || state.busy) return;
    state.loggingIn = true; showAccountError(""); renderAccountButtons();
    try { renderAccount(await api.LoginDiscord()); }
    catch (e) { showAccountError(errText(e)); }
    finally { state.loggingIn = false; renderAccountButtons(); updatePlay(); }
  }
  async function logout() {
    if (state.busy) return;
    showAccountError("");
    try { renderAccount(await api.Logout()); } catch (e) { showAccountError(errText(e)); }
  }

  // ---------- settings ----------
  function openSettings() {
    $("in-game").value = (state.cfg && state.cfg.gameDir) || "";
    state.found = []; state.selected = "";
    renderFound(); searchNote("");
    $("modal-settings").hidden = false;
    $("in-game").focus();
  }
  function closeSettings() {
    if (state.searching) api.CancelSearch();
    $("modal-settings").hidden = true;
  }

  async function saveSettings(ev) {
    ev.preventDefault();
    try {
      state.cfg = await api.SaveConfig({ ...state.cfg, gameDir: $("in-game").value.trim() });
      closeSettings();
      setStatus("");
      toast("Settings saved.", true);
    } catch (e) { toast(errText(e)); }
  }

  async function browse() {
    try {
      const dir = await api.PickGameDir();
      if (dir) { $("in-game").value = dir; state.selected = ""; renderFound(); }
    } catch (e) { toast(errText(e)); }
  }

  // ---------- search ----------
  // note = text under the field; busy = show the spinner and the Cancel button.
  function searchNote(text, busy) {
    $("search-status").hidden = !text;
    $("search-text").textContent = text || "";
    $("search-spinner").hidden = !busy;
    $("btn-search-cancel").hidden = !busy;
  }

  function renderFound() {
    const ul = $("candidates");
    ul.innerHTML = "";
    state.found.forEach((c) => {
      const li = document.createElement("li");
      li.className = "cand" + (c.supported ? "" : " is-dim") + (state.selected && state.selected.toLowerCase() === c.path.toLowerCase() ? " is-selected" : "");
      li.innerHTML = `<span class="cand-mark"></span><span class="cand-path" title="${esc(c.path)}">${esc(c.path)}</span><span class="cand-src ${c.supported ? "ok" : "bad"}">${c.supported ? "supported" : "wrong version"}</span>`;
      li.addEventListener("click", () => { $("in-game").value = c.path; state.selected = c.path; renderFound(); });
      ul.appendChild(li);
    });
    ul.hidden = state.found.length === 0;
  }

  async function search() {
    if (state.searching) return;
    state.searching = true; state.found = []; state.selected = "";
    $("btn-search").disabled = true; renderFound(); searchNote("Searching your drives…", true);
    try {
      const r = await api.SearchInstalls();
      if (r.cancelled) { searchNote(""); return; }
      state.found = r.installs || [];
      const ok = state.found.filter((c) => c.supported);
      if (ok.length === 1) { $("in-game").value = ok[0].path; state.selected = ok[0].path; }
      renderFound();
      let note = state.found.length === 0 ? "No New World install found. Use Browse… to pick the folder."
        : ok.length === 0 ? "Found New World, but not the version Azoth needs."
        : ok.length === 1 ? "Found it." : "Pick the install you want to use.";
      if (!r.complete) note += " The search stopped early; some folders were not checked.";
      searchNote(note);
    } catch (e) { searchNote(errText(e)); }
    finally { state.searching = false; $("btn-search").disabled = false; }
  }

  // ---------- events ----------
  bus.on("app:busy", (b) => setBusy(b));
  bus.on("account:changed", renderAccount);
  bus.on("play:status", (s) => { if (state.busy && !state.loggingIn) setStatus(s); });

  $("btn-min").addEventListener("click", win.minimise);
  $("btn-close").addEventListener("click", win.quit);
  $("btn-settings").addEventListener("click", openSettings);
  $("btn-settings-cancel").addEventListener("click", closeSettings);
  $("form-settings").addEventListener("submit", saveSettings);
  $("btn-browse").addEventListener("click", browse);
  $("btn-search").addEventListener("click", search);
  $("btn-search-cancel").addEventListener("click", () => api.CancelSearch());
  $("btn-login").addEventListener("click", loginDiscord);
  $("btn-login-cancel").addEventListener("click", () => api.CancelLogin());
  $("btn-logout").addEventListener("click", logout);
  $("btn-play").addEventListener("click", launch);
  document.addEventListener("keydown", (e) => { if (e.key === "Escape" && !$("modal-settings").hidden) closeSettings(); });
  if (real) document.addEventListener("contextmenu", (e) => e.preventDefault());

  // ---------- start ----------
  (async function init() {
    $("foot-version").textContent = "v" + (await api.GetVersion());
    state.cfg = await api.GetConfig();
    renderAccount(await api.GetAccount());

    // visual review shortcuts (outside Wails only): ?screen=settings&run=search
    if (!real && params && params.get("screen") === "settings") {
      openSettings();
      if (params.get("run") === "search") search();
    }
  })().catch((e) => { console.error(e); toast("Failed to start: " + errText(e)); });
})();
