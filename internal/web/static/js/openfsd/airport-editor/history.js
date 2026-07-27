/**
 * Pure undo/redo history stack for the airport editor.
 * Snapshot-based; gesture coalescing for drags. No DOM.
 *
 * Dirty flags are not stored in snapshots — recompute via
 * syncDirtyAfterHistoryApply after apply (K5 / K13).
 */

import { formatAptForDownload, formatAirForDownload } from './download.js';
import { hashText, syncDirtyFromHash } from './model.js';

/** @typedef {import('./model.js').EditorDocument} EditorDocument */
/** @typedef {import('./model.js').Airport} Airport */
/** @typedef {import('./model.js').Aircraft} Aircraft */

/**
 * @typedef {Object} EditorSnapshot
 * @property {Airport|null} airport
 * @property {Aircraft[]} aircraft
 * @property {{ type: string, index: number }|null} selection
 * @property {number} _nextSurfaceId
 */

/**
 * @typedef {Object} HistoryStack
 * @property {EditorSnapshot[]} undoStack
 * @property {EditorSnapshot[]} redoStack
 * @property {EditorSnapshot|null} gestureBaseline
 * @property {number} maxDepth
 */

export const DEFAULT_HISTORY_MAX_DEPTH = 100;

/**
 * Deep-clone JSON-safe values. Prefer structuredClone; fallback JSON.
 * @template T
 * @param {T} value
 * @returns {T}
 */
function deepClone(value) {
  if (value === null || value === undefined) return value;
  if (typeof structuredClone === 'function') {
    try {
      return structuredClone(value);
    } catch {
      /* fall through to JSON */
    }
  }
  return JSON.parse(JSON.stringify(value));
}

/**
 * @param {{ maxDepth?: number }} [opts]
 * @returns {HistoryStack}
 */
export function createHistory(opts = {}) {
  const n = Math.floor(Number(opts.maxDepth));
  const maxDepth =
    Number.isFinite(n) && n > 0 ? n : DEFAULT_HISTORY_MAX_DEPTH;
  return {
    undoStack: [],
    redoStack: [],
    gestureBaseline: null,
    maxDepth,
  };
}

/**
 * Deep-clone document content fields into a snapshot.
 * Mutating live `doc` after capture must not affect the returned snap.
 * @param {EditorDocument} doc
 * @returns {EditorSnapshot}
 */
export function captureSnapshot(doc) {
  return {
    airport: deepClone(doc.airport ?? null),
    aircraft: deepClone(Array.isArray(doc.aircraft) ? doc.aircraft : []),
    selection: deepClone(doc.selection ?? null),
    _nextSurfaceId:
      typeof doc._nextSurfaceId === 'number' && Number.isFinite(doc._nextSurfaceId)
        ? doc._nextSurfaceId
        : 1,
  };
}

/**
 * Apply snapshot into live doc (mutates doc). Does NOT touch dirty hashes,
 * filenames, mode, or serverValidation.
 * Deep-clones snap into doc so stack and live doc never share identity (K9).
 * @param {EditorDocument} doc
 * @param {EditorSnapshot} snap
 */
export function applySnapshot(doc, snap) {
  doc.airport = deepClone(snap.airport ?? null);
  doc.aircraft = deepClone(Array.isArray(snap.aircraft) ? snap.aircraft : []);
  doc.selection = deepClone(snap.selection ?? null);
  doc._nextSurfaceId =
    typeof snap._nextSurfaceId === 'number' && Number.isFinite(snap._nextSurfaceId)
      ? snap._nextSurfaceId
      : 1;
}

/**
 * Push a pre-mutation snapshot onto undo; clear redo; trim maxDepth.
 * Always pushes — no equality skip on this path (v1).
 * @param {HistoryStack} h
 * @param {EditorSnapshot} snap
 */
export function pushUndo(h, snap) {
  h.undoStack.push(snap);
  h.redoStack.length = 0;
  while (h.undoStack.length > h.maxDepth) {
    h.undoStack.shift();
  }
}

/**
 * Record current doc as undo entry (convenience).
 * Must be called BEFORE mutating doc for a discrete edit.
 * @param {HistoryStack} h
 * @param {EditorDocument} doc
 */
export function recordBeforeMutation(h, doc) {
  pushUndo(h, captureSnapshot(doc));
}

/**
 * Start a multi-event gesture (vertex/aircraft drag).
 * Captures baseline once; ignores nested begins.
 * @param {HistoryStack} h
 * @param {EditorDocument} doc
 */
export function beginGesture(h, doc) {
  if (h.gestureBaseline) return;
  h.gestureBaseline = captureSnapshot(doc);
}

/**
 * End gesture: if live doc content differs from baseline, push baseline to undo.
 * Always clears gestureBaseline. Returns whether an entry was pushed.
 * @param {HistoryStack} h
 * @param {EditorDocument} doc
 * @returns {boolean}
 */
export function commitGesture(h, doc) {
  const base = h.gestureBaseline;
  h.gestureBaseline = null;
  if (!base) return false;
  if (contentEquals(base, captureSnapshot(doc))) return false;
  pushUndo(h, base);
  return true;
}

/**
 * Abort gesture without stacking. Does not restore doc.
 * @param {HistoryStack} h
 */
export function discardGesture(h) {
  h.gestureBaseline = null;
}

/**
 * Undo: push current doc onto redo, apply top of undo onto doc.
 * @param {HistoryStack} h
 * @param {EditorDocument} doc
 * @returns {{ ok: true, snap: EditorSnapshot } | { ok: false, reason: string }}
 */
export function undo(h, doc) {
  if (h.gestureBaseline) {
    return { ok: false, reason: 'gesture-active' };
  }
  if (!h.undoStack.length) return { ok: false, reason: 'empty' };
  const current = captureSnapshot(doc);
  const prev = h.undoStack.pop();
  h.redoStack.push(current);
  applySnapshot(doc, prev);
  return { ok: true, snap: prev };
}

/**
 * Redo: inverse of undo.
 * @param {HistoryStack} h
 * @param {EditorDocument} doc
 * @returns {{ ok: true, snap: EditorSnapshot } | { ok: false, reason: string }}
 */
export function redo(h, doc) {
  if (h.gestureBaseline) {
    return { ok: false, reason: 'gesture-active' };
  }
  if (!h.redoStack.length) return { ok: false, reason: 'empty' };
  const current = captureSnapshot(doc);
  const next = h.redoStack.pop();
  h.undoStack.push(current);
  // Trim undo if redo branch grew past max (symmetric with pushUndo trim).
  while (h.undoStack.length > h.maxDepth) {
    h.undoStack.shift();
  }
  applySnapshot(doc, next);
  return { ok: true, snap: next };
}

/**
 * @param {HistoryStack} h
 * @returns {boolean}
 */
export function canUndo(h) {
  return h.undoStack.length > 0 && !h.gestureBaseline;
}

/**
 * @param {HistoryStack} h
 * @returns {boolean}
 */
export function canRedo(h) {
  return h.redoStack.length > 0 && !h.gestureBaseline;
}

/**
 * Empty both stacks and clear active gesture.
 * @param {HistoryStack} h
 */
export function clearHistory(h) {
  h.undoStack.length = 0;
  h.redoStack.length = 0;
  h.gestureBaseline = null;
}

/**
 * Stable content key for airport + aircraft only (ignore selection).
 * @param {EditorSnapshot} snap
 * @returns {string}
 */
export function snapshotContentKey(snap) {
  return JSON.stringify({ airport: snap.airport, aircraft: snap.aircraft });
}

/**
 * Structural content equality for gesture no-op detection.
 * Compares airport + aircraft only (selection ignored).
 * @param {EditorSnapshot} a
 * @param {EditorSnapshot} b
 * @returns {boolean}
 */
export function contentEquals(a, b) {
  return snapshotContentKey(a) === snapshotContentKey(b);
}

/**
 * Seed last*DownloadHash from download-format text. Does not set dirty true.
 * Leave dirty flags false when seeding a clean baseline (caller responsibility).
 * Never sets hashes to null.
 * @param {EditorDocument} doc
 * @param {'apt'|'air'|'both'} [side='both']
 */
export function seedCleanContentHashes(doc, side = 'both') {
  if (side === 'apt' || side === 'both') {
    doc.lastAptDownloadHash = hashText(formatAptForDownload(doc));
  }
  if (side === 'air' || side === 'both') {
    doc.lastAirDownloadHash = hashText(formatAirForDownload(doc));
  }
}

/**
 * Recompute aptDirty/airDirty from live last*DownloadHash vs download-format text.
 * Does not mutate hashes. Safe for null airport / empty aircraft.
 * @param {EditorDocument} doc
 */
export function syncDirtyAfterHistoryApply(doc) {
  syncDirtyFromHash(doc, 'apt', formatAptForDownload(doc));
  syncDirtyFromHash(doc, 'air', formatAirForDownload(doc));
}
