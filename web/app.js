// Renders the portfolio from /api/apps and keeps statuses live over
// Server-Sent Events from /api/stream.
(() => {
  "use strict";

  const $ = (sel, root = document) => root.querySelector(sel);
  const state = { data: null, filter: "All", rendered: false };

  // ---------- theme ----------
  const root = document.documentElement;
  $(".theme").addEventListener("click", () => {
    const dark = root.dataset.theme
      ? root.dataset.theme === "dark"
      : matchMedia("(prefers-color-scheme: dark)").matches;
    root.dataset.theme = dark ? "light" : "dark";
    try { localStorage.setItem("theme", root.dataset.theme); } catch {}
  });

  // Smooth-scroll only when someone clicks an in-page link. Smooth scrolling
  // set in CSS would also animate the jump to a #section on page load.
  const reduceMotion = matchMedia("(prefers-reduced-motion: reduce)").matches;
  document.addEventListener("click", (e) => {
    const a = e.target.closest('a[href^="#"]');
    const target = a && document.getElementById(a.getAttribute("href").slice(1));
    if (!target) return;
    e.preventDefault();
    scrollTo({ top: target.getBoundingClientRect().top + scrollY - 72, behavior: reduceMotion ? "instant" : "smooth" });
    history.pushState(null, "", a.getAttribute("href"));
  });

  const nav = $(".nav");
  addEventListener("scroll", () => nav.classList.toggle("scrolled", scrollY > 8), { passive: true });
  $("#year").textContent = new Date().getFullYear();

  // ---------- helpers ----------
  const STATUS_TEXT = { up: "Online", slow: "Online, slow", down: "Down", unknown: "Checking" };

  function host(url) {
    try { return new URL(url).host.replace(/^www\./, ""); } catch { return url; }
  }

  function ago(iso) {
    if (!iso) return "";
    const s = Math.max(0, Math.round((Date.now() - new Date(iso)) / 1000));
    if (s < 45) return "just now";
    const m = Math.round(s / 60);
    if (m < 60) return `${m} min ago`;
    const h = Math.round(m / 60);
    return `${h} hr ago`;
  }

  function setPill(el, app) {
    el.replaceChildren();
    if (!app.monitored) {
      if (app.badge) {
        el.dataset.status = "store";
        el.textContent = app.badge;
      } else {
        delete el.dataset.status;
      }
      return;
    }
    const h = app.health || { status: "unknown" };
    el.dataset.status = h.status;
    el.append(STATUS_TEXT[h.status] || "Checking");
    if ((h.status === "up" || h.status === "slow") && h.latencyMs) {
      const ms = document.createElement("span");
      ms.className = "ms";
      ms.textContent = `${h.latencyMs} ms`;
      el.append(ms);
    }
    el.title = h.lastChecked ? `Checked ${ago(h.lastChecked)}` : "Not checked yet";
  }

  function fillTags(ul, stack = []) {
    ul.replaceChildren(...stack.map((s) => Object.assign(document.createElement("li"), { textContent: s })));
  }

  function fillImage(img, app) {
    img.alt = `Screenshot of ${app.name}`;
    if (app.image) img.src = "/" + app.image;
  }

  // ---------- render: featured + grid (once) ----------
  function renderWork(apps) {
    const featured = $("#featured");
    const tplF = $("#tpl-featured").content;
    for (const app of apps.filter((a) => a.featured)) {
      const el = tplF.cloneNode(true).firstElementChild;
      el.dataset.id = app.id;
      el.classList.add("reveal");
      $(".shot", el).href = app.url;
      $(".shot", el).setAttribute("aria-label", `Open ${app.name}`);
      $(".addr", el).textContent = host(app.url);
      fillImage($("img", el), app);
      $(".cat", el).textContent = app.category;
      $("h3", el).textContent = app.name;
      $(".tagline", el).textContent = app.tagline;
      $(".desc", el).textContent = app.description;
      fillTags($(".tags", el), app.stack);
      $(".open", el).href = app.url;
      const src = $(".src", el);
      if (app.source) src.href = app.source; else src.remove();
      const cs = $(".case", el);
      if (app.caseStudy) cs.href = app.caseStudy; else cs.remove();
      featured.append(el);
    }

    const grid = $("#grid");
    const tplC = $("#tpl-card").content;
    const rest = apps.filter((a) => !a.featured);
    for (const app of rest) {
      const el = tplC.cloneNode(true).firstElementChild;
      el.dataset.id = app.id;
      el.dataset.category = app.category;
      el.classList.add("reveal");
      $(".shot", el).href = app.url;
      $(".shot", el).setAttribute("aria-label", `Open ${app.name}`);
      fillImage($("img", el), app);
      $(".cat", el).textContent = app.category;
      const title = $(".title", el);
      title.textContent = app.name;
      title.href = app.url;
      $(".desc", el).textContent = app.description;
      fillTags($(".tags", el), app.stack);
      grid.append(el);
    }

    const cats = ["All", ...new Set(rest.map((a) => a.category))];
    const filters = $("#filters");
    for (const c of cats) {
      const b = document.createElement("button");
      b.type = "button";
      b.textContent = c;
      b.setAttribute("aria-pressed", String(c === state.filter));
      b.addEventListener("click", () => {
        state.filter = c;
        for (const x of filters.children) x.setAttribute("aria-pressed", String(x === b));
        for (const card of grid.children) card.hidden = c !== "All" && card.dataset.category !== c;
      });
      filters.append(b);
    }

    revealOnScroll();
  }

  function revealOnScroll() {
    const els = document.querySelectorAll(".reveal");
    if (!("IntersectionObserver" in window)) {
      els.forEach((e) => e.classList.add("in"));
      return;
    }
    const io = new IntersectionObserver((entries) => {
      for (const e of entries) {
        if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); }
      }
    }, { rootMargin: "0px 0px -8% 0px" });
    els.forEach((e) => io.observe(e));
  }

  // ---------- render: live parts (every update) ----------
  function renderLive(data) {
    for (const app of data.apps) {
      document.querySelectorAll(`[data-id="${app.id}"] .pill`).forEach((p) => setPill(p, app));
    }
    if (data.static) return;
    renderPulse(data);
    renderBoard(data);
  }

  function renderPulse(data) {
    const { monitored, online, uptime7d } = data.overall;
    const dot = $("#pulse .dot");
    const text = $("#pulse-text");
    const checked = data.apps.filter((a) => a.monitored && a.health.status !== "unknown").length;
    if (checked < monitored) {
      dot.dataset.status = "unknown";
      text.textContent = checked === 0
        ? `Checking ${monitored} live apps…`
        : `Checking live apps… ${online} of ${checked} online so far`;
      return;
    }
    dot.dataset.status = online === monitored ? "up" : online === 0 ? "down" : "slow";
    text.replaceChildren();
    const strong = document.createElement("strong");
    strong.textContent = online === monitored ? `All ${monitored} apps online` : `${online} of ${monitored} apps online`;
    text.append(strong, " right now");
    if (uptime7d != null) text.append(` · ${uptime7d}% uptime this week`);
  }

  function renderBoard(data) {
    const rows = $("#board-rows");
    const monitored = data.apps.filter((a) => a.monitored);
    rows.replaceChildren(...monitored.map((app) => {
      const h = app.health;
      const row = document.createElement("div");
      row.className = "board-row";
      row.setAttribute("role", "row");

      const name = document.createElement("a");
      name.className = "name";
      name.href = app.url;
      name.target = "_blank";
      name.rel = "noopener";
      name.textContent = app.name;
      name.setAttribute("role", "cell");

      const now = document.createElement("span");
      now.className = "now";
      now.setAttribute("role", "cell");
      const label = document.createElement("span");
      label.className = "now-label";
      const dot = document.createElement("span");
      dot.className = "dot";
      dot.dataset.status = h.status;
      label.append(dot, STATUS_TEXT[h.status] || "Checking");
      now.append(label);

      const bars = document.createElement("span");
      bars.className = "bars";
      bars.setAttribute("role", "cell");
      const known = h.hours.filter((x) => x.uptime != null);
      bars.setAttribute("aria-label", known.length
        ? `${known.filter((x) => x.status === "up").length} of ${known.length} checked hours fully online`
        : "No checks yet");
      for (const hour of h.hours) {
        const i = document.createElement("i");
        i.dataset.status = hour.status;
        const t = new Date(hour.start).toLocaleTimeString([], { hour: "numeric" });
        i.title = hour.uptime == null ? `${t}: no data` : `${t}: ${hour.uptime}% online`;
        bars.append(i);
      }

      const up = document.createElement("span");
      up.className = "up num";
      up.setAttribute("role", "cell");
      up.textContent = h.uptime24h == null ? "–" : `${h.uptime24h}%`;

      const resp = document.createElement("span");
      resp.className = "resp num";
      resp.setAttribute("role", "cell");
      resp.textContent = h.avgLatency24h ? `${h.avgLatency24h} ms` : "–";

      row.append(name, now, bars, up, resp);
      return row;
    }));
    updateChecked();
  }

  function updateChecked() {
    const el = $("#checked");
    if (!el || !state.data || state.data.static) return; // no status section in the static build
    const secs = state.data.intervalSeconds;
    const every = secs >= 60 ? `every ${Math.round(secs / 60)} min` : `every ${secs} sec`;
    el.textContent = state.data.lastRun ? `Checked ${ago(state.data.lastRun)} · ${every}` : `First check running… · ${every}`;
  }
  setInterval(updateChecked, 15000);

  // ---------- data ----------
  function apply(data) {
    state.data = data;
    if (!state.rendered) {
      renderWork(data.apps);
      state.rendered = true;
      renderLive(data);
      // The sections above a #link only get their height now, so re-apply it.
      // Fonts can still be loading and shift things, so jump again once they're in.
      const target = location.hash && document.getElementById(location.hash.slice(1));
      if (target) {
        let userMoved = false;
        const stop = () => { userMoved = true; };
        for (const ev of ["wheel", "touchstart", "keydown", "mousedown"]) addEventListener(ev, stop, { once: true, passive: true });
        const jump = () => {
          if (!userMoved) scrollTo({ top: target.getBoundingClientRect().top + scrollY - 72, behavior: "instant" });
        };
        jump();
        document.fonts?.addEventListener("loadingdone", () => requestAnimationFrame(jump));
        addEventListener("load", () => requestAnimationFrame(jump), { once: true });
        setTimeout(jump, 800);
      }
      return;
    }
    renderLive(data);
  }

  async function load() {
    // The static build (scripts/build-static.sh) marks the page, so it
    // doesn't ask for a Go server that isn't there.
    if (document.documentElement.hasAttribute("data-static")) return loadStatic();
    try {
      const res = await fetch("/api/apps");
      if (res.ok && (res.headers.get("content-type") || "").includes("json")) {
        apply(await res.json());
        connect();
        return;
      }
    } catch {}
    loadStatic();
  }

  // Static mode: hosted without the Go server (for example on Vercel).
  // The app list comes straight from apps.json and the live status parts
  // are hidden, since there's nothing checking the apps.
  async function loadStatic() {
    document.body.classList.add("static");
    for (const el of document.querySelectorAll("[data-live]")) el.remove();
    try {
      const res = await fetch("/apps.json");
      if (!res.ok) throw new Error(res.status);
      const { apps } = await res.json();
      apply({ static: true, apps: apps.map((a) => ({ ...a, monitored: false })) });
    } catch {
      $("#featured").textContent = "Couldn't load the apps. Try refreshing.";
    }
  }

  function connect() {
    if (!("EventSource" in window)) {
      setInterval(() => fetch("/api/apps").then((r) => r.json()).then(apply).catch(() => {}), 60000);
      return;
    }
    // EventSource reconnects on its own if the connection drops.
    const es = new EventSource("/api/stream");
    es.addEventListener("status", (e) => {
      try { apply(JSON.parse(e.data)); } catch {}
    });
  }

  load();
})();
