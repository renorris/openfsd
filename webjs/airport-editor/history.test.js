import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  DEFAULT_HISTORY_MAX_DEPTH,
  createHistory,
  captureSnapshot,
  applySnapshot,
  pushUndo,
  recordBeforeMutation,
  beginGesture,
  commitGesture,
  discardGesture,
  undo,
  redo,
  canUndo,
  canRedo,
  clearHistory,
  contentEquals,
  snapshotContentKey,
  seedCleanContentHashes,
  syncDirtyAfterHistoryApply,
} from '../../internal/web/static/js/openfsd/airport-editor/history.js';
import {
  createEmptyDocument,
  createEmptyAirport,
  setAirport,
  setAircraft,
  addSurface,
  setVertex,
  deleteSurface,
  addAircraft,
  updateAircraft,
  noteDownload,
  markClean,
  hashText,
  SurfaceParking,
  SurfaceTaxiway,
  createDefaultAircraft,
} from '../../internal/web/static/js/openfsd/airport-editor/model.js';
import {
  formatAptForDownload,
  formatAirForDownload,
} from '../../internal/web/static/js/openfsd/airport-editor/download.js';

/** Build a small APT with one parking and one taxiway for mutations. */
function seedDocWithGeometry() {
  const doc = createEmptyDocument();
  const apt = createEmptyAirport();
  apt.icao = 'KBTV';
  apt.fieldElev = 335;
  setAirport(doc, apt, { markDirty: false });
  addSurface(doc, {
    kind: SurfaceParking,
    name: 'G1',
    points: [{ lat: 44.47, lon: -73.15 }],
  });
  addSurface(doc, {
    kind: SurfaceTaxiway,
    name: 'A',
    points: [
      { lat: 44.47, lon: -73.15 },
      { lat: 44.48, lon: -73.14 },
    ],
  });
  doc.aptDirty = false;
  doc.selection = { type: 'surface', index: 1 };
  return doc;
}

// ---------------------------------------------------------------------------
// createHistory / constants
// ---------------------------------------------------------------------------

test('DEFAULT_HISTORY_MAX_DEPTH is 100', () => {
  assert.equal(DEFAULT_HISTORY_MAX_DEPTH, 100);
});

test('createHistory defaults and custom maxDepth', () => {
  const h = createHistory();
  assert.deepEqual(h.undoStack, []);
  assert.deepEqual(h.redoStack, []);
  assert.equal(h.gestureBaseline, null);
  assert.equal(h.maxDepth, DEFAULT_HISTORY_MAX_DEPTH);

  const h2 = createHistory({ maxDepth: 3 });
  assert.equal(h2.maxDepth, 3);

  // Invalid / non-positive / non-finite → default
  assert.equal(createHistory({ maxDepth: 0 }).maxDepth, DEFAULT_HISTORY_MAX_DEPTH);
  assert.equal(createHistory({ maxDepth: 0.5 }).maxDepth, DEFAULT_HISTORY_MAX_DEPTH);
  assert.equal(createHistory({ maxDepth: -2 }).maxDepth, DEFAULT_HISTORY_MAX_DEPTH);
  assert.equal(createHistory({ maxDepth: Infinity }).maxDepth, DEFAULT_HISTORY_MAX_DEPTH);
  assert.equal(createHistory({ maxDepth: NaN }).maxDepth, DEFAULT_HISTORY_MAX_DEPTH);
  assert.equal(createHistory({ maxDepth: 'nope' }).maxDepth, DEFAULT_HISTORY_MAX_DEPTH);
  // Floor then accept: 2.9 → 2
  assert.equal(createHistory({ maxDepth: 2.9 }).maxDepth, 2);
});

// ---------------------------------------------------------------------------
// capture / apply isolation (K9)
// ---------------------------------------------------------------------------

test('captureSnapshot deep-clones; live mutation does not affect snap', () => {
  const doc = seedDocWithGeometry();
  const snap = captureSnapshot(doc);

  assert.equal(snap.airport.icao, 'KBTV');
  assert.equal(snap.aircraft.length, 0);
  assert.deepEqual(snap.selection, { type: 'surface', index: 1 });
  assert.equal(snap._nextSurfaceId, doc._nextSurfaceId);

  // Mutate live doc
  doc.airport.icao = 'XXXX';
  doc.airport.surfaces[1].points[0].lat = 99;
  doc.selection = null;
  doc._nextSurfaceId = 999;
  doc.aircraft.push({ callsign: 'N1' });

  assert.equal(snap.airport.icao, 'KBTV');
  assert.equal(snap.airport.surfaces[1].points[0].lat, 44.47);
  assert.deepEqual(snap.selection, { type: 'surface', index: 1 });
  assert.notEqual(snap._nextSurfaceId, 999);
  assert.equal(snap.aircraft.length, 0);
});

test('applySnapshot deep-clones into doc; stack snap remains isolated', () => {
  const doc = seedDocWithGeometry();
  const h = createHistory();
  const before = captureSnapshot(doc);
  pushUndo(h, before);

  // Mutate to a different state
  doc.airport.icao = 'KXXX';
  setVertex(doc, 1, 0, { lat: 50, lon: -70 });
  doc.selection = { type: 'surface', index: 0 };

  const res = undo(h, doc);
  assert.equal(res.ok, true);
  assert.equal(doc.airport.icao, 'KBTV');
  assert.equal(doc.airport.surfaces[1].points[0].lat, 44.47);

  // Mutate live after apply — must not alter redo stack snaps (K9)
  doc.airport.icao = 'MUTATED';
  doc.airport.surfaces[0].points[0].lat = 1;
  const stacked = h.redoStack[0];
  assert.equal(stacked.airport.icao, 'KXXX');
  assert.equal(stacked.airport.surfaces[1].points[0].lat, 50);
  assert.equal(stacked.airport.surfaces[0].points[0].lat, 44.47);

  // Mutating live doc must not alter the originally captured `before` snap either
  // (before was pushed to undo then popped — still held by test via `before`)
  assert.equal(before.airport.icao, 'KBTV');
  assert.equal(before.airport.surfaces[1].points[0].lat, 44.47);

  // Redo restores KXXX from stack clone; further live mutation does not corrupt it
  const r2 = redo(h, doc);
  assert.equal(r2.ok, true);
  assert.equal(doc.airport.icao, 'KXXX');
  // undo stack now has capture of pre-redo live state (MUTATED)
  const u = h.undoStack[h.undoStack.length - 1];
  assert.equal(u.airport.icao, 'MUTATED');
  doc.airport.icao = 'AGAIN';
  doc.airport.surfaces[0].name = 'ZZZ';
  assert.equal(u.airport.icao, 'MUTATED');
  assert.equal(u.airport.surfaces[0].name, 'G1');
  // redo stack empty; original `before` still pristine
  assert.equal(before.airport.icao, 'KBTV');
  assert.equal(before.airport.surfaces[0].name, 'G1');
});

test('applySnapshot does not touch dirty hashes, filenames, mode', () => {
  const doc = seedDocWithGeometry();
  doc.aptDirty = true;
  doc.airDirty = true;
  doc.lastAptDownloadHash = 'deadbeef';
  doc.lastAirDownloadHash = 'cafebabe';
  doc.aptFilename = 'keep.apt';
  doc.airFilename = 'keep.air';
  doc.mode = 'taxi';
  doc.serverValidation = { loading: false, stale: false };

  const empty = captureSnapshot(createEmptyDocument());
  applySnapshot(doc, empty);

  assert.equal(doc.airport, null);
  assert.equal(doc.aptDirty, true);
  assert.equal(doc.airDirty, true);
  assert.equal(doc.lastAptDownloadHash, 'deadbeef');
  assert.equal(doc.lastAirDownloadHash, 'cafebabe');
  assert.equal(doc.aptFilename, 'keep.apt');
  assert.equal(doc.airFilename, 'keep.air');
  assert.equal(doc.mode, 'taxi');
  assert.deepEqual(doc.serverValidation, { loading: false, stale: false });
});

// ---------------------------------------------------------------------------
// push / record / undo / redo
// ---------------------------------------------------------------------------

test('recordBefore + mutate + undo restores; redo restores post-state', () => {
  const doc = seedDocWithGeometry();
  const h = createHistory();
  const icaoBefore = doc.airport.icao;

  recordBeforeMutation(h, doc);
  doc.airport.icao = 'KXXX';
  doc.aptDirty = true;

  assert.equal(canUndo(h), true);
  assert.equal(canRedo(h), false);

  const u = undo(h, doc);
  assert.equal(u.ok, true);
  assert.equal(doc.airport.icao, icaoBefore);
  assert.equal(canRedo(h), true);

  const r = redo(h, doc);
  assert.equal(r.ok, true);
  assert.equal(doc.airport.icao, 'KXXX');
});

test('pushUndo always grows stack (no identical-top elision)', () => {
  const doc = seedDocWithGeometry();
  const h = createHistory();
  const a = captureSnapshot(doc);
  const b = captureSnapshot(doc);
  pushUndo(h, a);
  pushUndo(h, b);
  assert.equal(h.undoStack.length, 2);
  assert.ok(contentEquals(h.undoStack[0], h.undoStack[1]));
});

test('redo cleared on new pushUndo after undo branch', () => {
  const doc = seedDocWithGeometry();
  const h = createHistory();

  recordBeforeMutation(h, doc);
  doc.airport.icao = 'A1';
  undo(h, doc);
  assert.equal(canRedo(h), true);

  recordBeforeMutation(h, doc);
  doc.airport.icao = 'A2';
  assert.equal(canRedo(h), false);
  assert.equal(h.redoStack.length, 0);
});

test('undo/redo empty and gesture-active reasons', () => {
  const doc = createEmptyDocument();
  const h = createHistory();

  assert.deepEqual(undo(h, doc), { ok: false, reason: 'empty' });
  assert.deepEqual(redo(h, doc), { ok: false, reason: 'empty' });

  beginGesture(h, doc);
  assert.deepEqual(undo(h, doc), { ok: false, reason: 'gesture-active' });
  assert.deepEqual(redo(h, doc), { ok: false, reason: 'gesture-active' });
});

// ---------------------------------------------------------------------------
// maxDepth
// ---------------------------------------------------------------------------

test('maxDepth drops oldest on overflow', () => {
  const h = createHistory({ maxDepth: 3 });
  const doc = createEmptyDocument();

  for (let i = 0; i < 5; i++) {
    doc._nextSurfaceId = i + 1;
    pushUndo(h, captureSnapshot(doc));
  }
  assert.equal(h.undoStack.length, 3);
  // Oldest remaining should be from when _nextSurfaceId was 3 (0-based loop: i=2 → id 3)
  assert.equal(h.undoStack[0]._nextSurfaceId, 3);
  assert.equal(h.undoStack[2]._nextSurfaceId, 5);
});

test('maxDepth trims undo on redo path when stack would exceed max', () => {
  // Normal undo/redo from a max-sized stack never exceeds max (undo shrinks
  // undo as it grows redo). Cover the defensive trim in redo by pre-filling
  // undo to maxDepth with a pending redo entry so redo would push past max.
  const h = createHistory({ maxDepth: 2 });
  const doc = createEmptyDocument();

  doc._nextSurfaceId = 1;
  h.undoStack.push(captureSnapshot(doc));
  doc._nextSurfaceId = 2;
  h.undoStack.push(captureSnapshot(doc));
  doc._nextSurfaceId = 99;
  h.redoStack.push(captureSnapshot(doc));
  doc._nextSurfaceId = 3; // live present; redo captures this onto undo

  const r = redo(h, doc);
  assert.equal(r.ok, true);
  assert.equal(doc._nextSurfaceId, 99);
  assert.equal(h.undoStack.length, 2); // not 3 — oldest dropped
  assert.equal(h.undoStack[0]._nextSurfaceId, 2);
  assert.equal(h.undoStack[1]._nextSurfaceId, 3);
  assert.equal(h.redoStack.length, 0);
});

// ---------------------------------------------------------------------------
// gestures
// ---------------------------------------------------------------------------

test('beginGesture / commitGesture coalesces multi-move into one undo entry', () => {
  const doc = seedDocWithGeometry();
  const h = createHistory();
  const lat0 = doc.airport.surfaces[1].points[0].lat;

  beginGesture(h, doc);
  setVertex(doc, 1, 0, { lat: lat0 + 0.01, lon: -73.15 });
  beginGesture(h, doc); // nested no-op
  setVertex(doc, 1, 0, { lat: lat0 + 0.02, lon: -73.14 });
  const pushed = commitGesture(h, doc);
  assert.equal(pushed, true);
  assert.equal(h.undoStack.length, 1);
  assert.equal(h.gestureBaseline, null);

  undo(h, doc);
  assert.equal(doc.airport.surfaces[1].points[0].lat, lat0);
});

test('commitGesture no-op when content unchanged (selection-only ignored)', () => {
  const doc = seedDocWithGeometry();
  const h = createHistory();

  beginGesture(h, doc);
  doc.selection = { type: 'surface', index: 0 }; // selection change only
  const pushed = commitGesture(h, doc);
  assert.equal(pushed, false);
  assert.equal(h.undoStack.length, 0);
});

test('commitGesture with no begin returns false', () => {
  const doc = createEmptyDocument();
  const h = createHistory();
  assert.equal(commitGesture(h, doc), false);
});

test('discardGesture clears baseline without stacking', () => {
  const doc = seedDocWithGeometry();
  const h = createHistory();
  beginGesture(h, doc);
  setVertex(doc, 1, 0, { lat: 99, lon: 0 });
  discardGesture(h);
  assert.equal(h.gestureBaseline, null);
  assert.equal(h.undoStack.length, 0);
});

test('canUndo / canRedo false during active gesture', () => {
  const doc = seedDocWithGeometry();
  const h = createHistory();
  recordBeforeMutation(h, doc);
  doc.airport.icao = 'X';
  undo(h, doc); // leave something on redo
  assert.equal(canUndo(h), false); // empty undo after single undo
  // put one undo back
  recordBeforeMutation(h, doc);
  assert.equal(canUndo(h), true);
  assert.equal(canRedo(h), false);

  beginGesture(h, doc);
  assert.equal(canUndo(h), false);
  assert.equal(canRedo(h), false);
  discardGesture(h);
  assert.equal(canUndo(h), true);
});

// ---------------------------------------------------------------------------
// clearHistory
// ---------------------------------------------------------------------------

test('clearHistory empties stacks and gesture', () => {
  const doc = seedDocWithGeometry();
  const h = createHistory();
  recordBeforeMutation(h, doc);
  beginGesture(h, doc);
  // after begin, push another path: force redo empty, undo has 1, gesture set
  clearHistory(h);
  assert.equal(h.undoStack.length, 0);
  assert.equal(h.redoStack.length, 0);
  assert.equal(h.gestureBaseline, null);
});

// ---------------------------------------------------------------------------
// contentEquals / selection + _nextSurfaceId
// ---------------------------------------------------------------------------

test('contentEquals true for deep-equal geometry, false when vertex moves', () => {
  const doc = seedDocWithGeometry();
  const a = captureSnapshot(doc);
  const b = captureSnapshot(doc);
  assert.equal(contentEquals(a, b), true);

  b.selection = { type: 'aircraft', index: 0 };
  assert.equal(contentEquals(a, b), true); // selection ignored

  b.airport.surfaces[1].points[0].lat = 0;
  assert.equal(contentEquals(a, b), false);

  assert.equal(
    snapshotContentKey(a),
    JSON.stringify({ airport: a.airport, aircraft: a.aircraft }),
  );
});

test('selection and _nextSurfaceId restore after undo of delete / add surface', () => {
  const doc = seedDocWithGeometry();
  const h = createHistory();
  const nextIdBefore = doc._nextSurfaceId;
  doc.selection = { type: 'surface', index: 0 };

  recordBeforeMutation(h, doc);
  deleteSurface(doc, 0);
  assert.equal(doc.selection, null); // deleteSurface clears selection at index

  undo(h, doc);
  assert.deepEqual(doc.selection, { type: 'surface', index: 0 });
  assert.equal(doc.airport.surfaces.length, 2);
  assert.equal(doc.airport.surfaces[0].name, 'G1');
  assert.equal(doc._nextSurfaceId, nextIdBefore);

  // add surface path
  recordBeforeMutation(h, doc);
  const idBeforeAdd = doc._nextSurfaceId;
  addSurface(doc, {
    kind: SurfaceParking,
    name: 'G2',
    points: [{ lat: 1, lon: 2 }],
  });
  assert.ok(doc._nextSurfaceId > idBeforeAdd || doc.airport.surfaces.length === 3);

  undo(h, doc);
  assert.equal(doc.airport.surfaces.length, 2);
  assert.equal(doc._nextSurfaceId, idBeforeAdd);
});

test('null airport baseline: place then undo restores airport null', () => {
  const doc = createEmptyDocument();
  const h = createHistory();
  const before = captureSnapshot(doc);
  assert.equal(before.airport, null);

  pushUndo(h, before);
  setAirport(doc, createEmptyAirport(), { markDirty: true });
  addSurface(doc, {
    kind: SurfaceParking,
    name: 'G1',
    points: [{ lat: 1, lon: 2 }],
  });

  undo(h, doc);
  assert.equal(doc.airport, null);
});

// ---------------------------------------------------------------------------
// Dirty / baseline helpers (K13)
// ---------------------------------------------------------------------------

test('bootstrap / empty seed: mutate then undo → clean via syncDirtyAfterHistoryApply', () => {
  const doc = createEmptyDocument();
  seedCleanContentHashes(doc, 'both');
  assert.ok(doc.lastAptDownloadHash);
  assert.ok(doc.lastAirDownloadHash);
  assert.equal(doc.aptDirty, false);

  const h = createHistory();
  recordBeforeMutation(h, doc);
  setAirport(doc, createEmptyAirport(), { markDirty: true });
  addSurface(doc, {
    kind: SurfaceParking,
    name: 'G1',
    points: [{ lat: 44.47, lon: -73.15 }],
  });
  assert.equal(doc.aptDirty, true);

  undo(h, doc);
  syncDirtyAfterHistoryApply(doc);
  assert.equal(doc.airport, null);
  assert.equal(doc.aptDirty, false);
  assert.equal(doc.airDirty, false);
});

test('open-equivalent seed: set airport + seed apt → mutate → undo → clean', () => {
  const doc = createEmptyDocument();
  const apt = createEmptyAirport();
  apt.icao = 'KBTV';
  setAirport(doc, apt, { markDirty: false });
  seedCleanContentHashes(doc, 'apt');
  doc.aptDirty = false;

  const h = createHistory();
  recordBeforeMutation(h, doc);
  doc.airport.icao = 'KXXX';
  doc.aptDirty = true;

  undo(h, doc);
  syncDirtyAfterHistoryApply(doc);
  assert.equal(doc.airport.icao, 'KBTV');
  assert.equal(doc.aptDirty, false);
});

test('download then undo/redo dirty tracking', () => {
  const doc = seedDocWithGeometry();
  seedCleanContentHashes(doc, 'both');
  doc.aptDirty = false;

  const h = createHistory();
  recordBeforeMutation(h, doc);
  doc.airport.icao = 'KXXX';
  doc.aptDirty = true;

  // Download current (edited) content — marks clean with hash of KXXX
  noteDownload(doc, 'apt', formatAptForDownload(doc));
  assert.equal(doc.aptDirty, false);

  undo(h, doc);
  syncDirtyAfterHistoryApply(doc);
  assert.equal(doc.airport.icao, 'KBTV');
  assert.equal(doc.aptDirty, true); // hash(A) ≠ hash(B)

  redo(h, doc);
  syncDirtyAfterHistoryApply(doc);
  assert.equal(doc.airport.icao, 'KXXX');
  assert.equal(doc.aptDirty, false);
});

test('mark-clean then undo may re-dirty via recompute', () => {
  const doc = seedDocWithGeometry();
  seedCleanContentHashes(doc, 'both');
  doc.aptDirty = false;

  const h = createHistory();
  recordBeforeMutation(h, doc);
  doc.airport.icao = 'KXXX';
  doc.aptDirty = true;

  // Mark clean without changing hash (hash still seed of KBTV content)
  markClean(doc, 'apt');
  assert.equal(doc.aptDirty, false);

  // Current content is KXXX; recompute would dirty — but we only recompute after history apply
  // Undo to KBTV which matches seed → clean
  undo(h, doc);
  syncDirtyAfterHistoryApply(doc);
  assert.equal(doc.airport.icao, 'KBTV');
  assert.equal(doc.aptDirty, false);

  // Redo to KXXX vs seed KBTV → dirty
  redo(h, doc);
  syncDirtyAfterHistoryApply(doc);
  assert.equal(doc.airport.icao, 'KXXX');
  assert.equal(doc.aptDirty, true);
});

test('null airport dirty symmetry when hash is non-null from prior download', () => {
  const doc = seedDocWithGeometry();
  const text = formatAptForDownload(doc);
  noteDownload(doc, 'apt', text);
  assert.equal(doc.aptDirty, false);
  assert.ok(doc.lastAptDownloadHash);

  const emptySnap = captureSnapshot(createEmptyDocument());
  applySnapshot(doc, emptySnap);
  syncDirtyAfterHistoryApply(doc);
  assert.equal(doc.airport, null);
  assert.equal(doc.aptDirty, true);
  assert.notEqual(hashText(''), doc.lastAptDownloadHash);
});

test('seed recipe unity: seed and syncDirty use download-format helpers', () => {
  const doc = createEmptyDocument();
  seedCleanContentHashes(doc, 'both');
  const expectedApt = hashText(formatAptForDownload(doc));
  const expectedAir = hashText(formatAirForDownload(doc));
  assert.equal(doc.lastAptDownloadHash, expectedApt);
  assert.equal(doc.lastAirDownloadHash, expectedAir);
  // empty download format is ''
  assert.equal(formatAptForDownload(doc), '');
  assert.equal(formatAirForDownload(doc), '');
  assert.equal(expectedApt, hashText(''));
  assert.equal(expectedAir, hashText(''));

  syncDirtyAfterHistoryApply(doc);
  assert.equal(doc.aptDirty, false);
  assert.equal(doc.airDirty, false);
});

test('seedCleanContentHashes side=apt leaves air hash alone', () => {
  const doc = createEmptyDocument();
  doc.lastAirDownloadHash = 'keepme!!';
  seedCleanContentHashes(doc, 'apt');
  assert.equal(doc.lastAirDownloadHash, 'keepme!!');
  assert.equal(doc.lastAptDownloadHash, hashText(formatAptForDownload(doc)));
});

test('seedCleanContentHashes forces dirty false for seeded side (Open after dirty)', () => {
  // setAirport({ markDirty: false }) only skips setting dirty true — does not clear.
  const doc = createEmptyDocument();
  doc.aptDirty = true;
  doc.airDirty = true;
  seedCleanContentHashes(doc, 'apt');
  assert.equal(doc.aptDirty, false);
  assert.equal(doc.airDirty, true); // air side not seeded
  seedCleanContentHashes(doc, 'air');
  assert.equal(doc.airDirty, false);
  doc.aptDirty = true;
  doc.airDirty = true;
  seedCleanContentHashes(doc, 'both');
  assert.equal(doc.aptDirty, false);
  assert.equal(doc.airDirty, false);
});

test('air side: record / undo / seed clean for aircraft', () => {
  const doc = createEmptyDocument();
  const apt = createEmptyAirport();
  apt.icao = 'KBTV';
  setAirport(doc, apt, { markDirty: false });
  setAircraft(doc, [createDefaultAircraft(apt, { lat: 44.47, lon: -73.15 })], {
    markDirty: false,
  });
  seedCleanContentHashes(doc, 'both');
  doc.airDirty = false;

  const h = createHistory();
  recordBeforeMutation(h, doc);
  updateAircraft(doc, 0, { callsign: 'CHANGED' });

  undo(h, doc);
  syncDirtyAfterHistoryApply(doc);
  assert.notEqual(doc.aircraft[0].callsign, 'CHANGED');
  assert.equal(doc.airDirty, false);
});
