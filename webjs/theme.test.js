/**
 * Unit tests for openfsd theme.js pure helpers (classic script → OpenFSDTheme).
 */
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { createContext, runInContext } from "node:vm";
import assert from "node:assert/strict";
import { describe, it } from "node:test";

const __dirname = dirname(fileURLToPath(import.meta.url));
const themePath = join(
  __dirname,
  "../internal/web/static/js/openfsd/theme.js"
);
const themeSource = readFileSync(themePath, "utf8");

/**
 * Load theme.js into an isolated context without auto-binding a toggle.
 * Boot still runs against the provided documentElement.
 */
function loadTheme(opts = {}) {
  const attrs = new Map();
  const documentElement = {
    setAttribute(name, value) {
      attrs.set(name, String(value));
    },
    getAttribute(name) {
      return attrs.has(name) ? attrs.get(name) : null;
    },
  };

  const store = new Map(opts.storeEntries || []);
  const localStorage = {
    getItem(k) {
      return store.has(k) ? store.get(k) : null;
    },
    setItem(k, v) {
      store.set(k, String(v));
    },
  };

  const prefersDark = !!opts.prefersDark;
  const doc = {
    documentElement,
    readyState: "complete",
    querySelector() {
      return opts.toggleBtn || null;
    },
    addEventListener() {},
  };
  const win = {
    localStorage,
    matchMedia() {
      return {
        matches: prefersDark,
        addEventListener() {},
        addListener() {},
      };
    },
  };

  const sandbox = { globalThis: null, document: doc, window: win };
  sandbox.globalThis = sandbox;
  runInContext(themeSource, createContext(sandbox));

  return {
    api: sandbox.OpenFSDTheme,
    attrs,
    store,
    documentElement,
    localStorage,
    doc,
    win,
  };
}

describe("OpenFSDTheme", () => {
  it("exports the public API", () => {
    const { api } = loadTheme();
    assert.equal(typeof api.resolveTheme, "function");
    assert.equal(typeof api.applyTheme, "function");
    assert.equal(typeof api.nextTheme, "function");
    assert.equal(api.STORAGE_KEY, "openfsd-theme");
  });

  it("resolveTheme prefers stored light/dark over system", () => {
    const { api } = loadTheme();
    assert.equal(api.resolveTheme("dark", false), "dark");
    assert.equal(api.resolveTheme("light", true), "light");
    assert.equal(api.resolveTheme(null, true), "dark");
    assert.equal(api.resolveTheme(null, false), "light");
    assert.equal(api.resolveTheme("bogus", true), "dark");
    assert.equal(api.resolveTheme("", false), "light");
  });

  it("normalizeTheme accepts only light|dark", () => {
    const { api } = loadTheme();
    assert.equal(api.normalizeTheme("dark"), "dark");
    assert.equal(api.normalizeTheme("light"), "light");
    assert.equal(api.normalizeTheme("auto"), null);
    assert.equal(api.normalizeTheme(null), null);
  });

  it("nextTheme toggles", () => {
    const { api } = loadTheme();
    assert.equal(api.nextTheme("light"), "dark");
    assert.equal(api.nextTheme("dark"), "light");
  });

  it("applyTheme sets data-bs-theme on the root", () => {
    const { api, attrs, documentElement } = loadTheme();
    assert.equal(api.applyTheme("dark", documentElement), "dark");
    assert.equal(attrs.get("data-bs-theme"), "dark");
    assert.equal(api.applyTheme("nope", documentElement), "light");
    assert.equal(attrs.get("data-bs-theme"), "light");
  });

  it("readStored/writeStored round-trip and tolerate missing storage", () => {
    const { api, localStorage } = loadTheme();
    assert.equal(api.readStored(null), null);
    assert.equal(api.readStored(localStorage), null);
    api.writeStored(localStorage, "dark");
    assert.equal(api.readStored(localStorage), "dark");
    assert.equal(api.readStored({}), null);
    api.writeStored(null, "light"); // no throw
  });

  it("systemPrefersDark mirrors MediaQueryList.matches", () => {
    const { api } = loadTheme();
    assert.equal(api.systemPrefersDark(null), false);
    assert.equal(api.systemPrefersDark({ matches: true }), true);
    assert.equal(api.systemPrefersDark({ matches: false }), false);
  });

  it("syncToggle updates aria-label and pressed", () => {
    const { api } = loadTheme();
    const btnAttrs = new Map();
    const btn = {
      setAttribute(k, v) {
        btnAttrs.set(k, String(v));
      },
    };
    api.syncToggle(btn, "dark");
    assert.equal(btnAttrs.get("aria-pressed"), "true");
    assert.match(btnAttrs.get("aria-label"), /light/i);
    api.syncToggle(btn, "light");
    assert.equal(btnAttrs.get("aria-pressed"), "false");
    assert.match(btnAttrs.get("aria-label"), /dark/i);
  });

  it("boot applies stored preference on script load", () => {
    const { attrs } = loadTheme({
      storeEntries: [["openfsd-theme", "dark"]],
    });
    assert.equal(attrs.get("data-bs-theme"), "dark");
  });

  it("boot falls back to system preference when unset", () => {
    const { attrs } = loadTheme({ prefersDark: true });
    assert.equal(attrs.get("data-bs-theme"), "dark");
  });

  it("boot defaults to light without preference", () => {
    const { attrs } = loadTheme({ prefersDark: false });
    assert.equal(attrs.get("data-bs-theme"), "light");
  });

  it("osmBasemap returns light OSM and dark CARTO configs", () => {
    const { api } = loadTheme();
    const light = api.osmBasemap("light");
    const dark = api.osmBasemap("dark");
    assert.ok(light.url.includes("openstreetmap.org"));
    assert.match(light.attribution, /OpenStreetMap/i);
    assert.ok(dark.url.includes("cartocdn") || dark.url.includes("dark"));
    assert.match(dark.attribution, /OpenStreetMap/i);
    assert.match(dark.attribution, /CARTO/i);
    assert.equal(dark.subdomains, "abcd");
  });

  it("createLeafletOsmLayer passes theme-specific url to L.tileLayer", () => {
    const { api } = loadTheme();
    const calls = [];
    const L = {
      tileLayer(url, opts) {
        calls.push({ url, opts });
        return { url, opts };
      },
    };
    api.createLeafletOsmLayer(L, "light");
    api.createLeafletOsmLayer(L, "dark");
    assert.equal(calls.length, 2);
    assert.ok(calls[0].url.includes("openstreetmap.org"));
    assert.equal(calls[0].opts.maxZoom, 19);
    assert.ok(calls[1].url.includes("cartocdn") || calls[1].url.includes("dark"));
    assert.equal(calls[1].opts.subdomains, "abcd");
  });

  it("applyTheme dispatches openfsd:themechange", () => {
    const events = [];
    const attrs = new Map();
    const documentElement = {
      setAttribute(name, value) {
        attrs.set(name, String(value));
      },
      getAttribute(name) {
        return attrs.has(name) ? attrs.get(name) : null;
      },
      ownerDocument: null,
    };
    const doc = {
      documentElement,
      readyState: "complete",
      querySelector() {
        return null;
      },
      addEventListener() {},
      dispatchEvent(ev) {
        events.push(ev);
        return true;
      },
    };
    documentElement.ownerDocument = doc;
    // Minimal CustomEvent for vm
    function CustomEvent(type, init) {
      this.type = type;
      this.detail = init && init.detail;
      this.bubbles = !!(init && init.bubbles);
    }
    const store = new Map();
    const win = {
      localStorage: {
        getItem(k) {
          return store.has(k) ? store.get(k) : null;
        },
        setItem(k, v) {
          store.set(k, String(v));
        },
      },
      matchMedia() {
        return { matches: false, addEventListener() {}, addListener() {} };
      },
      CustomEvent,
    };
    const sandbox = {
      globalThis: null,
      document: doc,
      window: win,
      CustomEvent,
    };
    sandbox.globalThis = sandbox;
    runInContext(themeSource, createContext(sandbox));
    // boot already applied light once
    const afterBoot = events.length;
    sandbox.OpenFSDTheme.applyTheme("dark", documentElement);
    assert.ok(events.length > afterBoot);
    const last = events[events.length - 1];
    assert.equal(last.type, "openfsd:themechange");
    assert.equal(last.detail.theme, "dark");
    assert.equal(attrs.get("data-bs-theme"), "dark");
  });
});
