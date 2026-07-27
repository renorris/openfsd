/**
 * openfsd color theme — Bootstrap 5.3 data-bs-theme + localStorage.
 *
 * Classic (non-module) script: load synchronously in <head> so the first paint
 * already has the preferred theme (avoids light→dark flash). Toggle is
 * progressive enhancement; without JS the page stays on the default light theme.
 *
 * Public API: globalThis.OpenFSDTheme
 */
(function (root) {
  "use strict";

  var STORAGE_KEY = "openfsd-theme";
  /** Dispatched on document after data-bs-theme is applied (maps listen for basemap swap). */
  var THEME_CHANGE_EVENT = "openfsd:themechange";

  /**
   * OSM basemap configs (light = OSM Standard; dark = CARTO Dark Matter, OSM data).
   * Shared by dashboard + airport editor Leaflet maps.
   */
  var OSM_BASEMAP_LIGHT = {
    url: "https://tile.openstreetmap.org/{z}/{x}/{y}.png",
    maxZoom: 19,
    attribution:
      '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
  };
  var OSM_BASEMAP_DARK = {
    url: "https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png",
    maxZoom: 20,
    subdomains: "abcd",
    attribution:
      '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> ' +
      '&copy; <a href="https://carto.com/attributions">CARTO</a>',
  };

  /**
   * @param {string|null|undefined} value
   * @returns {"light"|"dark"|null}
   */
  function normalizeTheme(value) {
    if (value === "dark" || value === "light") {
      return value;
    }
    return null;
  }

  /**
   * Resolve effective theme from stored preference and system preference.
   * @param {string|null|undefined} stored
   * @param {boolean} prefersDark
   * @returns {"light"|"dark"}
   */
  function resolveTheme(stored, prefersDark) {
    var n = normalizeTheme(stored);
    if (n) {
      return n;
    }
    return prefersDark ? "dark" : "light";
  }

  /**
   * @param {Storage|null|undefined} storage
   * @returns {string|null}
   */
  function readStored(storage) {
    try {
      if (!storage || typeof storage.getItem !== "function") {
        return null;
      }
      return storage.getItem(STORAGE_KEY);
    } catch (_) {
      return null;
    }
  }

  /**
   * @param {Storage|null|undefined} storage
   * @param {"light"|"dark"} theme
   */
  function writeStored(storage, theme) {
    try {
      if (!storage || typeof storage.setItem !== "function") {
        return;
      }
      storage.setItem(STORAGE_KEY, theme);
    } catch (_) {
      /* private mode / quota */
    }
  }

  /**
   * @param {MediaQueryList|null|undefined} mql
   * @returns {boolean}
   */
  function systemPrefersDark(mql) {
    return !!(mql && mql.matches);
  }

  /**
   * Read effective theme from a document (data-bs-theme on <html>).
   * @param {Document|null|undefined} doc
   * @returns {"light"|"dark"}
   */
  function currentTheme(doc) {
    var el = doc && doc.documentElement;
    var v =
      el && typeof el.getAttribute === "function"
        ? el.getAttribute("data-bs-theme")
        : null;
    return normalizeTheme(v) || "light";
  }

  /**
   * OSM tile URL + Leaflet options for the given UI theme.
   * @param {"light"|"dark"|string} theme
   * @returns {{ url: string, maxZoom: number, attribution: string, subdomains?: string }}
   */
  function osmBasemap(theme) {
    return theme === "dark" ? OSM_BASEMAP_DARK : OSM_BASEMAP_LIGHT;
  }

  /**
   * Build a Leaflet OSM (or dark OSM) tile layer for the current UI theme.
   * @param {*} L Leaflet global
   * @param {"light"|"dark"|string} [theme]
   * @returns {*} Leaflet tile layer
   */
  function createLeafletOsmLayer(L, theme) {
    var cfg = osmBasemap(theme === "dark" ? "dark" : "light");
    var opts = {
      maxZoom: cfg.maxZoom,
      attribution: cfg.attribution,
    };
    if (cfg.subdomains) {
      opts.subdomains = cfg.subdomains;
    }
    return L.tileLayer(cfg.url, opts);
  }

  /**
   * Notify listeners (maps) that the UI theme changed.
   * @param {Document|null|undefined} doc
   * @param {"light"|"dark"} theme
   */
  function dispatchThemeChange(doc, theme) {
    if (!doc || typeof doc.dispatchEvent !== "function") {
      return;
    }
    try {
      var Ev =
        typeof CustomEvent === "function"
          ? CustomEvent
          : doc.defaultView && doc.defaultView.CustomEvent;
      if (typeof Ev !== "function") {
        return;
      }
      doc.dispatchEvent(
        new Ev(THEME_CHANGE_EVENT, {
          detail: { theme: theme },
          bubbles: true,
        })
      );
    } catch (_) {
      /* ignore */
    }
  }

  /**
   * Apply theme to a document root (html element).
   * @param {"light"|"dark"|string} theme
   * @param {Element|null|undefined} rootEl
   * @returns {"light"|"dark"}
   */
  function applyTheme(theme, rootEl) {
    var t = theme === "dark" ? "dark" : "light";
    if (rootEl && typeof rootEl.setAttribute === "function") {
      rootEl.setAttribute("data-bs-theme", t);
      var doc = rootEl.ownerDocument || null;
      dispatchThemeChange(doc, t);
    }
    return t;
  }

  /**
   * @param {"light"|"dark"} current
   * @returns {"light"|"dark"}
   */
  function nextTheme(current) {
    return current === "dark" ? "light" : "dark";
  }

  /**
   * Update toggle accessible name / pressed state.
   * @param {Element|null|undefined} btn
   * @param {"light"|"dark"} theme
   */
  function syncToggle(btn, theme) {
    if (!btn || typeof btn.setAttribute !== "function") {
      return;
    }
    var dark = theme === "dark";
    btn.setAttribute("aria-pressed", dark ? "true" : "false");
    btn.setAttribute(
      "aria-label",
      dark ? "Switch to light mode" : "Switch to dark mode"
    );
    btn.setAttribute("title", dark ? "Light mode" : "Dark mode");
  }

  /**
   * Read system preference when matchMedia is available.
   * @param {Window|null|undefined} win
   * @returns {boolean}
   */
  function prefersDarkFromWindow(win) {
    if (!win || typeof win.matchMedia !== "function") {
      return false;
    }
    try {
      return systemPrefersDark(win.matchMedia("(prefers-color-scheme: dark)"));
    } catch (_) {
      return false;
    }
  }

  /**
   * Apply stored/system theme to document.
   * @param {Document} doc
   * @param {Window} win
   * @returns {"light"|"dark"}
   */
  function boot(doc, win) {
    var storage = win && win.localStorage ? win.localStorage : null;
    var theme = resolveTheme(readStored(storage), prefersDarkFromWindow(win));
    applyTheme(theme, doc && doc.documentElement);
    return theme;
  }

  /**
   * Wire [data-js="theme-toggle"] and keep in sync with system when no stored pref.
   * @param {Document} doc
   * @param {Window} win
   */
  function bind(doc, win) {
    if (!doc || !win) {
      return;
    }

    var storage = win.localStorage || null;
    var btn = doc.querySelector('[data-js="theme-toggle"]');
    var current =
      normalizeTheme(
        doc.documentElement && doc.documentElement.getAttribute("data-bs-theme")
      ) || "light";
    syncToggle(btn, current);

    if (btn && typeof btn.addEventListener === "function") {
      btn.addEventListener("click", function () {
        var cur =
          normalizeTheme(doc.documentElement.getAttribute("data-bs-theme")) ||
          "light";
        var next = nextTheme(cur);
        applyTheme(next, doc.documentElement);
        writeStored(storage, next);
        syncToggle(btn, next);
      });
    }

    // Follow OS only when the user has not chosen an explicit theme.
    if (typeof win.matchMedia === "function") {
      try {
        var mql = win.matchMedia("(prefers-color-scheme: dark)");
        var onChange = function () {
          if (normalizeTheme(readStored(storage))) {
            return;
          }
          var theme = resolveTheme(null, systemPrefersDark(mql));
          applyTheme(theme, doc.documentElement);
          syncToggle(btn, theme);
        };
        if (typeof mql.addEventListener === "function") {
          mql.addEventListener("change", onChange);
        } else if (typeof mql.addListener === "function") {
          mql.addListener(onChange);
        }
      } catch (_) {
        /* ignore */
      }
    }
  }

  var api = {
    STORAGE_KEY: STORAGE_KEY,
    THEME_CHANGE_EVENT: THEME_CHANGE_EVENT,
    OSM_BASEMAP_LIGHT: OSM_BASEMAP_LIGHT,
    OSM_BASEMAP_DARK: OSM_BASEMAP_DARK,
    normalizeTheme: normalizeTheme,
    resolveTheme: resolveTheme,
    readStored: readStored,
    writeStored: writeStored,
    systemPrefersDark: systemPrefersDark,
    currentTheme: currentTheme,
    osmBasemap: osmBasemap,
    createLeafletOsmLayer: createLeafletOsmLayer,
    dispatchThemeChange: dispatchThemeChange,
    applyTheme: applyTheme,
    nextTheme: nextTheme,
    syncToggle: syncToggle,
    boot: boot,
    bind: bind,
  };

  root.OpenFSDTheme = api;

  // Immediate apply when loaded as a classic script in the browser.
  if (typeof document !== "undefined" && typeof window !== "undefined") {
    boot(document, window);
    if (document.readyState === "loading") {
      document.addEventListener("DOMContentLoaded", function () {
        bind(document, window);
      });
    } else {
      bind(document, window);
    }
  }
})(typeof globalThis !== "undefined" ? globalThis : this);
