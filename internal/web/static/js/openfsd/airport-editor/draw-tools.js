/**
 * Draw-mode state machine (pure where possible).
 * Map click / finish / cancel orchestration for park/taxi/rwy/hold/aircraft.
 *
 * Does not touch DOM except optional prompt injection for names.
 */

import {
  SurfaceParking,
  SurfaceRunway,
  SurfaceTaxiway,
  SurfaceHold,
  MODE_SELECT,
  MODE_PARK,
  MODE_TAXI,
  MODE_RUNWAY,
  MODE_HOLD,
  MODE_AIRCRAFT,
  addSurface,
  addAircraft,
  createDefaultAircraft,
  nextParkingName,
  nextPathName,
  ensureAirport,
  setMode,
} from './model.js';
import { isWordName, isTaxiHoldName, isRunwayDesignator } from './parse-apt.js';

/**
 * @typedef {Object} DrawSession
 * @property {string|null} kind - PARKING|TAXIWAY|RUNWAY|HOLD|AIRCRAFT|null when idle
 * @property {{ lat: number, lon: number }[]} points
 * @property {boolean} active
 */

/**
 * @returns {DrawSession}
 */
export function createDrawSession() {
  return { kind: null, points: [], active: false };
}

/**
 * Whether mode is a multi-click polyline draw (taxi/rwy/hold multi).
 * Hold accepts 1+ points; taxi/rwy need ≥2 to finish.
 * @param {string} mode
 * @returns {boolean}
 */
export function isPolylineMode(mode) {
  return mode === MODE_TAXI || mode === MODE_RUNWAY || mode === MODE_HOLD;
}

/**
 * Whether mode places something on a single map click.
 * @param {string} mode
 * @returns {boolean}
 */
export function isClickPlaceMode(mode) {
  return mode === MODE_PARK || mode === MODE_AIRCRAFT;
}

/**
 * Map editor mode → surface kind (or AIRCRAFT).
 * @param {string} mode
 * @returns {string|null}
 */
export function modeToKind(mode) {
  switch (mode) {
    case MODE_PARK:
      return SurfaceParking;
    case MODE_TAXI:
      return SurfaceTaxiway;
    case MODE_RUNWAY:
      return SurfaceRunway;
    case MODE_HOLD:
      return SurfaceHold;
    case MODE_AIRCRAFT:
      return 'AIRCRAFT';
    default:
      return null;
  }
}

/**
 * Start or reset draw session for mode.
 * @param {DrawSession} session
 * @param {string} mode
 * @returns {DrawSession}
 */
export function beginDraw(session, mode) {
  const kind = modeToKind(mode);
  if (!kind || kind === 'AIRCRAFT' || mode === MODE_PARK) {
    // Single-click modes don't keep open polyline session.
    session.kind = kind;
    session.points = [];
    session.active = mode === MODE_PARK || mode === MODE_AIRCRAFT || isPolylineMode(mode);
    return session;
  }
  session.kind = kind;
  session.points = [];
  session.active = true;
  return session;
}

/**
 * Cancel draw session; returns true if something was cancelled.
 * @param {DrawSession} session
 * @returns {boolean}
 */
export function cancelDraw(session) {
  const was = session.active && (session.points.length > 0 || session.kind);
  session.kind = null;
  session.points = [];
  session.active = false;
  return !!was;
}

/**
 * Min points required to finish polyline for kind.
 * @param {string} kind
 * @returns {number}
 */
export function finishMinPoints(kind) {
  if (kind === SurfaceHold) return 1;
  if (kind === SurfaceTaxiway || kind === SurfaceRunway) return 2;
  return 1;
}

/**
 * Append vertex during polyline draw.
 * @param {DrawSession} session
 * @param {{ lat: number, lon: number }} pt
 * @returns {DrawSession}
 */
export function addDrawPoint(session, pt) {
  if (!session.active || !session.kind) return session;
  if (session.kind === 'AIRCRAFT' || session.kind === SurfaceParking) return session;
  session.points.push({ lat: pt.lat, lon: pt.lon });
  return session;
}

/**
 * Can finish current polyline?
 * @param {DrawSession} session
 * @returns {boolean}
 */
export function canFinishDraw(session) {
  if (!session.active || !session.kind) return false;
  if (session.kind === SurfaceParking || session.kind === 'AIRCRAFT') return false;
  return session.points.length >= finishMinPoints(session.kind);
}

/**
 * Build parking surface from click + name.
 * @param {string} name
 * @param {{ lat: number, lon: number }} pt
 * @returns {{ ok: true, surface: import('./model.js').Surface } | { ok: false, error: string }}
 */
export function buildParkingSurface(name, pt) {
  const n = String(name || '').trim().toUpperCase();
  if (!isWordName(n)) {
    return { ok: false, error: 'Parking name must be word characters (A–Z, 0–9, _).' };
  }
  return {
    ok: true,
    surface: {
      kind: SurfaceParking,
      name: n,
      points: [{ lat: pt.lat, lon: pt.lon }],
    },
  };
}

/**
 * Build taxi/hold surface from finished points.
 * @param {'TAXIWAY'|'HOLD'} kind
 * @param {string} name
 * @param {{ lat: number, lon: number }[]} points
 * @returns {{ ok: true, surface: import('./model.js').Surface } | { ok: false, error: string }}
 */
export function buildPathSurface(kind, name, points) {
  const n = String(name || '').trim().toUpperCase();
  if (!isTaxiHoldName(n)) {
    return { ok: false, error: 'Name must match taxi/hold pattern (letters + optional digits).' };
  }
  if (!points || points.length < finishMinPoints(kind)) {
    return { ok: false, error: `Need at least ${finishMinPoints(kind)} point(s).` };
  }
  return {
    ok: true,
    surface: {
      kind,
      name: n,
      points: points.map((p) => ({ lat: p.lat, lon: p.lon })),
    },
  };
}

/**
 * Build runway surface. Designators A/B prompted.
 * @param {string} rwyA
 * @param {string} rwyB
 * @param {{ lat: number, lon: number }[]} points
 * @returns {{ ok: true, surface: import('./model.js').Surface } | { ok: false, error: string }}
 */
export function buildRunwaySurface(rwyA, rwyB, points) {
  const a = String(rwyA || '').trim().toUpperCase();
  const b = String(rwyB || '').trim().toUpperCase();
  if (!isRunwayDesignator(a) || !isRunwayDesignator(b)) {
    return { ok: false, error: 'Runway ends must be designators like 19 or 1L (no leading zero).' };
  }
  if (!points || points.length < 2) {
    return { ok: false, error: 'Runway needs at least 2 points.' };
  }
  return {
    ok: true,
    surface: {
      kind: SurfaceRunway,
      name: `${a}/${b}`,
      rwyA: a,
      rwyB: b,
      dispA: 0,
      dispB: 0,
      turnoffLeft: true,
      points: points.map((p) => ({ lat: p.lat, lon: p.lon })),
    },
  };
}

/**
 * @typedef {Object} DrawPromptFns
 * @property {(msg: string, def: string) => string|null} [prompt] - return null to cancel
 */

/**
 * Handle map click for current doc mode.
 * Mutates doc / session. Returns status for UI.
 *
 * @param {import('./model.js').EditorDocument} doc
 * @param {DrawSession} session
 * @param {{ lat: number, lon: number }} latlng
 * @param {DrawPromptFns} [prompts]
 * @returns {{
 *   action: 'none'|'placed-park'|'placed-aircraft'|'vertex'|'finished'|'error'|'need-more',
 *   index?: number,
 *   message?: string,
 *   error?: string
 * }}
 */
export function handleMapClick(doc, session, latlng, prompts = {}) {
  const mode = doc.mode || MODE_SELECT;
  const promptFn =
    prompts.prompt ||
    ((msg, def) => {
      if (typeof globalThis.prompt === 'function') return globalThis.prompt(msg, def);
      return def;
    });

  if (mode === MODE_SELECT) {
    return { action: 'none' };
  }

  if (mode === MODE_PARK) {
    ensureAirport(doc);
    const def = nextParkingName(doc.airport);
    const name = promptFn('Parking name', def);
    if (name === null) return { action: 'none', message: 'Cancelled.' };
    const built = buildParkingSurface(name || def, latlng);
    if (!built.ok) return { action: 'error', error: built.error };
    const index = addSurface(doc, built.surface);
    doc.selection = { type: 'surface', index };
    return { action: 'placed-park', index, message: `Parking ${built.surface.name} added.` };
  }

  if (mode === MODE_AIRCRAFT) {
    const ac = createDefaultAircraft(doc.airport, latlng, doc.aircraft);
    const index = addAircraft(doc, ac);
    doc.selection = { type: 'aircraft', index };
    return {
      action: 'placed-aircraft',
      index,
      message: `Aircraft ${ac.callsign} placed (hdg ${ac.heading}).`,
    };
  }

  if (isPolylineMode(mode)) {
    if (!session.active || modeToKind(mode) !== session.kind) {
      beginDraw(session, mode);
    }
    addDrawPoint(session, latlng);
    const n = session.points.length;
    const need = finishMinPoints(session.kind);
    if (n < need) {
      return {
        action: 'need-more',
        message: `Vertex ${n} — need ≥${need}; double-click or Enter to finish.`,
      };
    }
    return {
      action: 'vertex',
      message: `Vertex ${n} — double-click or Enter to finish; Esc cancel.`,
    };
  }

  return { action: 'none' };
}

/**
 * Finish polyline draw session → add surface.
 * @param {import('./model.js').EditorDocument} doc
 * @param {DrawSession} session
 * @param {DrawPromptFns} [prompts]
 * @returns {{
 *   action: 'none'|'finished'|'error',
 *   index?: number,
 *   message?: string,
 *   error?: string
 * }}
 */
export function finishDraw(doc, session, prompts = {}) {
  if (!canFinishDraw(session)) {
    return {
      action: 'error',
      error: session.active
        ? `Need at least ${finishMinPoints(session.kind || '')} point(s).`
        : 'Nothing to finish.',
    };
  }
  const promptFn =
    prompts.prompt ||
    ((msg, def) => {
      if (typeof globalThis.prompt === 'function') return globalThis.prompt(msg, def);
      return def;
    });

  ensureAirport(doc);
  const kind = session.kind;
  const pts = session.points.slice();
  // Double-click often appends a near-duplicate final vertex — drop it.
  if (pts.length >= 2) {
    const a = pts[pts.length - 2];
    const b = pts[pts.length - 1];
    if (Math.abs(a.lat - b.lat) < 1e-9 && Math.abs(a.lon - b.lon) < 1e-9) {
      pts.pop();
    }
  }
  if (pts.length < finishMinPoints(kind || '')) {
    return {
      action: 'error',
      error: `Need at least ${finishMinPoints(kind || '')} point(s).`,
    };
  }

  /** @type {{ ok: true, surface: import('./model.js').Surface } | { ok: false, error: string }} */
  let built;
  if (kind === SurfaceRunway) {
    const a = promptFn('Runway end A designator (e.g. 19)', '19');
    if (a === null) return { action: 'none', message: 'Cancelled.' };
    const b = promptFn('Runway end B designator (e.g. 1)', '1');
    if (b === null) return { action: 'none', message: 'Cancelled.' };
    built = buildRunwaySurface(a, b, pts);
  } else if (kind === SurfaceTaxiway || kind === SurfaceHold) {
    const def = nextPathName(doc.airport, kind);
    const label = kind === SurfaceHold ? 'Hold name' : 'Taxiway name';
    const name = promptFn(label, def);
    if (name === null) return { action: 'none', message: 'Cancelled.' };
    built = buildPathSurface(kind, name || def, pts);
  } else {
    return { action: 'error', error: 'Unknown draw kind.' };
  }

  if (!built.ok) return { action: 'error', error: built.error };

  const index = addSurface(doc, built.surface);
  doc.selection = { type: 'surface', index };
  cancelDraw(session);
  setMode(doc, MODE_SELECT);
  return {
    action: 'finished',
    index,
    message: `${built.surface.kind} ${built.surface.name} added.`,
  };
}

/**
 * Escape → cancel draw and return to select.
 * @param {import('./model.js').EditorDocument} doc
 * @param {DrawSession} session
 * @returns {boolean} true if cancelled something / left draw mode
 */
export function escapeDraw(doc, session) {
  const cancelled = cancelDraw(session);
  const wasDraw = doc.mode !== MODE_SELECT;
  setMode(doc, MODE_SELECT);
  return cancelled || wasDraw;
}
