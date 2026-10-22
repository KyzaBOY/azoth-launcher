// Bridge between the screen and Go. Inside Wails, window.go.main.App and
// window.runtime exist; outside (opening index.html in a plain browser to
// review the look) sample data takes over so the screen keeps working alone.
//
// Mock switches (query string, browser only):
//   ?account=1            start logged in
//   ?screen=settings      open the settings dialog (add &run=search to run the search)
//   ?search=none|one|many what Search finds (default: many)
//   ?play=error|denied|pingblocked  how Play ends (default: success)
//   ?platform=linux       show the Linux settings and paths
(function () {
  "use strict";

  const params = new URLSearchParams(location.search);
  const real = !!(window.go && window.go.main && window.go.main.App);

  // ---- event bus: Wails' or a local one ------------------------------------
  const listeners = {};
  const bus = {
    on(name, cb) {
      if (window.runtime && window.runtime.EventsOn) return window.runtime.EventsOn(name, cb);
      (listeners[name] = listeners[name] || []).push(cb);
      return () => { listeners[name] = (listeners[name] || []).filter((f) => f !== cb); };
    },
    emitLocal(name, data) {
      (listeners[name] || []).forEach((cb) => { try { cb(data); } catch (e) { console.error(e); } });
    },
  };

  const win = {
    minimise() { if (window.runtime && window.runtime.WindowMinimise) window.runtime.WindowMinimise(); },
    quit() { if (window.runtime && window.runtime.Quit) window.runtime.Quit(); else console.log("[mock] Quit"); },
  };

  if (real) {
    window.AzothBridge = { real: true, api: window.go.main.App, bus, win, params };
    return;
  }

  // ---- sample mode ------------------------------------------------------------
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

  const cfg = { gameDir: "", copyDir: "", protonPath: "" };

  let loginCancelled = false;
  let searchCancelled = false;
  const demoUser = { id: "123456789012345678", name: "Player" };
  let acct = params.get("account") === "1"
    ? { loggedIn: true, ...demoUser, expires: Math.floor(Date.now() / 1000) + 30 * 86400 }
    : { loggedIn: false, id: "", name: "", expires: 0 };

  const hits = {
    none: [],
    one: [{ path: "D:\\Games\\New World", supported: true }],
    many: [
      { path: "D:\\Games\\New World", supported: true },
      { path: "E:\\SteamLibrary\\steamapps\\common\\New World", supported: false },
    ],
  };

  const api = {
    async GetVersion() { return "0.1.0"; },
    async GetPlatform() { return params.get("platform") || "windows"; },
    async GetAccount() { return { ...acct }; },
    async LoginDiscord() {
      bus.emitLocal("app:busy", true);
      loginCancelled = false;
      await sleep(1800);
      if (loginCancelled) { bus.emitLocal("app:busy", false); throw new Error("login cancelled"); }
      acct = { loggedIn: true, ...demoUser, expires: Math.floor(Date.now() / 1000) + 30 * 86400 };
      bus.emitLocal("app:busy", false);
      bus.emitLocal("account:changed", { ...acct });
      return { ...acct };
    },
    async CancelLogin() { loginCancelled = true; },
    async Logout() {
      acct = { loggedIn: false, id: "", name: "", expires: 0 };
      bus.emitLocal("account:changed", { ...acct });
      return { ...acct };
    },
    async GetConfig() { return { ...cfg }; },
    async SaveConfig(c) { Object.assign(cfg, c); return { ...cfg }; },
    async PickGameDir() { return ""; },
    async PickFolder() { return ""; },
    async DefaultCopyDir() { return "C:\\Users\\Player\\AppData\\Local\\AzothLauncher\\client"; },
    async SearchInstalls() {
      searchCancelled = false;
      for (let i = 0; i < 12 && !searchCancelled; i++) await sleep(150);
      if (searchCancelled) return { installs: [], complete: true, cancelled: true };
      return { installs: hits[params.get("search")] || hits.many, complete: true, cancelled: false };
    },
    async CancelSearch() { searchCancelled = true; },
    async Launch() {
      bus.emitLocal("app:busy", true);
      bus.emitLocal("play:status", "Preparing game files (first time only)…");
      await sleep(900);
      bus.emitLocal("play:status", "Starting…");
      await sleep(600);
      bus.emitLocal("app:busy", false);
      switch (params.get("play")) {
        case "error": return { ok: false, error: "This New World version isn't supported. Azoth needs build 1.400.6031." };
        case "denied": return { ok: false, error: "Your Discord account doesn't have access yet. The playtest is closed for now." };
        case "pingblocked": return { ok: true, notice: "Game started. The menu ping will show 9999 ms: this system blocks ping for normal users (net.ipv4.ping_group_range). See the README." };
        default: return { ok: true };
      }
    },
  };

  window.AzothBridge = { real: false, api, bus, win, params };
})();
