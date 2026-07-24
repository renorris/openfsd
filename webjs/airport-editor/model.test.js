import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  createEmptyDocument,
  createEmptyAirport,
  markAptDirty,
  markAirDirty,
  markClean,
  setAirport,
  setAircraft,
  findSurface,
  hashText,
  noteDownload,
  syncDirtyFromHash,
  setMode,
  MODE_TAXI,
  MODE_SELECT,
  nextParkingName,
  nextPathName,
  nextAircraftCallsign,
  createDefaultAircraft,
  ensureAirport,
  addSurface,
  updateSurface,
  deleteSurface,
  setVertex,
  insertVertex,
  deleteVertex,
  addAircraft,
  updateAircraft,
  deleteAircraft,
  snapAircraftToParking,
  updateAirportHeaders,
  resetDocument,
  hasParking,
  parkingSurfaces,
  minPointsForKind,
  SurfaceParking,
  SurfaceRunway,
  SurfaceTaxiway,
  SurfaceHold,
  DefaultRegistration,
  DEFAULT_PATTERN_SIZE,
  EnginePiston,
} from '../../internal/web/static/js/openfsd/airport-editor/model.js';

test('createEmptyDocument', () => {
  const doc = createEmptyDocument();
  assert.equal(doc.airport, null);
  assert.deepEqual(doc.aircraft, []);
  assert.equal(doc.aptDirty, false);
  assert.equal(doc.airDirty, false);
  assert.equal(doc.mode, 'select');
  assert.equal(doc.lastAptDownloadHash, null);
  assert.equal(doc.lastAirDownloadHash, null);
});

test('createEmptyAirport defaults', () => {
  const a = createEmptyAirport();
  assert.equal(a.patternSize, DEFAULT_PATTERN_SIZE);
  assert.equal(a.registration, DefaultRegistration);
  assert.deepEqual(a.surfaces, []);
});

test('mark dirty / clean', () => {
  const doc = createEmptyDocument();
  markAptDirty(doc);
  markAirDirty(doc);
  assert.equal(doc.aptDirty, true);
  assert.equal(doc.airDirty, true);
  markClean(doc, 'apt');
  assert.equal(doc.aptDirty, false);
  assert.equal(doc.airDirty, true);
  markClean(doc, 'both');
  assert.equal(doc.airDirty, false);
});

test('hashText stable and changes with content', () => {
  assert.equal(hashText('hello'), hashText('hello'));
  assert.notEqual(hashText('hello'), hashText('world'));
  assert.equal(hashText('').length, 8);
});

test('noteDownload clears dirty and stores hash', () => {
  const doc = createEmptyDocument();
  doc.aptDirty = true;
  noteDownload(doc, 'apt', 'icao=KBTV\n');
  assert.equal(doc.aptDirty, false);
  assert.equal(doc.lastAptDownloadHash, hashText('icao=KBTV\n'));
  doc.airDirty = true;
  noteDownload(doc, 'air', 'N1:C172:…');
  assert.equal(doc.airDirty, false);
  assert.ok(doc.lastAirDownloadHash);
});

test('syncDirtyFromHash', () => {
  const doc = createEmptyDocument();
  noteDownload(doc, 'apt', 'A');
  syncDirtyFromHash(doc, 'apt', 'A');
  assert.equal(doc.aptDirty, false);
  syncDirtyFromHash(doc, 'apt', 'B');
  assert.equal(doc.aptDirty, true);
});

test('setMode', () => {
  const doc = createEmptyDocument();
  setMode(doc, MODE_TAXI);
  assert.equal(doc.mode, MODE_TAXI);
  setMode(doc, 'nope');
  assert.equal(doc.mode, MODE_TAXI);
  setMode(doc, MODE_SELECT);
  assert.equal(doc.mode, MODE_SELECT);
});

test('setAirport setAircraft', () => {
  const doc = createEmptyDocument();
  const apt = createEmptyAirport();
  apt.icao = 'KBTV';
  setAirport(doc, apt, { filename: 'k.apt', errors: ['x'] });
  assert.equal(doc.airport.icao, 'KBTV');
  assert.equal(doc.aptFilename, 'k.apt');
  assert.deepEqual(doc.aptErrors, ['x']);
  assert.equal(doc.aptDirty, true);

  setAircraft(doc, [{ callsign: 'A1' }], { filename: 'k.air', errors: [], markDirty: false });
  assert.equal(doc.aircraft.length, 1);
  assert.equal(doc.airFilename, 'k.air');
  assert.equal(doc.airDirty, false);

  // Isolation: setAircraft copies array
  const src = [{ callsign: 'B1' }];
  setAircraft(doc, src);
  src.push({ callsign: 'C1' });
  assert.equal(doc.aircraft.length, 1);
});

test('findSurface', () => {
  assert.equal(findSurface(null, 'A'), null);
  const apt = {
    surfaces: [
      { kind: SurfaceParking, name: 'G1', points: [] },
      {
        kind: SurfaceRunway,
        name: '19/1',
        rwyA: '19',
        rwyB: '1',
        points: [],
      },
    ],
  };
  assert.equal(findSurface(apt, 'g1').name, 'G1');
  assert.equal(findSurface(apt, '19').name, '19/1');
  assert.equal(findSurface(apt, '1').name, '19/1');
  assert.equal(findSurface(apt, '19/1').name, '19/1');
  assert.equal(findSurface(apt, 'ZZ'), null);
});

test('nextParkingName skips used', () => {
  assert.equal(nextParkingName(null), 'P1');
  const apt = {
    surfaces: [
      { kind: SurfaceParking, name: 'P1', points: [] },
      { kind: SurfaceParking, name: 'P2', points: [] },
      { kind: SurfaceTaxiway, name: 'A', points: [] },
    ],
  };
  assert.equal(nextParkingName(apt), 'P3');
});

test('nextPathName taxi and hold', () => {
  assert.equal(nextPathName(null, SurfaceTaxiway), 'A');
  assert.equal(nextPathName(null, SurfaceHold), 'H1');
  const apt = {
    surfaces: [
      { kind: SurfaceTaxiway, name: 'A', points: [] },
      { kind: SurfaceHold, name: 'H1', points: [] },
    ],
  };
  assert.equal(nextPathName(apt, SurfaceTaxiway), 'B');
  assert.equal(nextPathName(apt, SurfaceHold), 'H2');
});

test('nextAircraftCallsign', () => {
  assert.equal(nextAircraftCallsign([]), 'N001');
  assert.equal(nextAircraftCallsign([{ callsign: 'N001' }]), 'N002');
});

test('createDefaultAircraft uses ICAO and elev', () => {
  const apt = createEmptyAirport();
  apt.icao = 'KBTV';
  apt.fieldElev = 335;
  const ac = createDefaultAircraft(apt, { lat: 44.47, lon: -73.15, heading: 90 }, []);
  assert.equal(ac.dep, 'KBTV');
  assert.equal(ac.arr, 'KBTV');
  assert.equal(ac.alt, 335);
  assert.equal(ac.heading, 90);
  assert.equal(ac.engine, EnginePiston);
  assert.equal(ac.lat, 44.47);
  assert.ok(ac.callsign);
});

test('surface mutations', () => {
  const doc = createEmptyDocument();
  ensureAirport(doc);
  const i = addSurface(doc, {
    kind: SurfaceTaxiway,
    name: 'A',
    points: [
      { lat: 1, lon: 2 },
      { lat: 3, lon: 4 },
    ],
  });
  assert.equal(i, 0);
  assert.equal(doc.aptDirty, true);
  assert.ok(doc.airport.surfaces[0].id);

  assert.equal(updateSurface(doc, 0, { name: 'B' }), true);
  assert.equal(doc.airport.surfaces[0].name, 'B');

  assert.equal(setVertex(doc, 0, 0, { lat: 9, lon: 8 }), true);
  assert.equal(doc.airport.surfaces[0].points[0].lat, 9);

  assert.equal(insertVertex(doc, 0, 0, { lat: 5, lon: 6 }), true);
  assert.equal(doc.airport.surfaces[0].points.length, 3);

  let res = deleteVertex(doc, 0, 1);
  assert.equal(res.ok, true);
  assert.equal(doc.airport.surfaces[0].points.length, 2);

  // Cannot go below min 2 for taxiway
  res = deleteVertex(doc, 0, 0);
  // still 2 points after one delete? we had 2, delete one would fail if min is 2
  // after previous delete we have 2; deleting again fails
  res = deleteVertex(doc, 0, 0);
  assert.equal(res.ok, false);

  doc.selection = { type: 'surface', index: 0 };
  assert.equal(deleteSurface(doc, 0), true);
  assert.equal(doc.airport.surfaces.length, 0);
  assert.equal(doc.selection, null);
});

test('aircraft mutations and snap', () => {
  const doc = createEmptyDocument();
  ensureAirport(doc);
  addSurface(doc, {
    kind: SurfaceParking,
    name: 'G1',
    points: [{ lat: 44.1, lon: -73.2 }],
  });
  const ai = addAircraft(doc, createDefaultAircraft(doc.airport, { lat: 0, lon: 0 }));
  assert.equal(ai, 0);
  assert.equal(doc.airDirty, true);

  assert.equal(updateAircraft(doc, 0, { heading: 180 }), true);
  assert.equal(doc.aircraft[0].heading, 180);

  assert.equal(snapAircraftToParking(doc, 0, 0), true);
  assert.equal(doc.aircraft[0].lat, 44.1);
  assert.equal(doc.aircraft[0].lon, -73.2);

  doc.selection = { type: 'aircraft', index: 0 };
  assert.equal(deleteAircraft(doc, 0), true);
  assert.equal(doc.aircraft.length, 0);
  assert.equal(doc.selection, null);
});

test('hasParking parkingSurfaces', () => {
  assert.equal(hasParking(null), false);
  const apt = createEmptyAirport();
  assert.equal(hasParking(apt), false);
  apt.surfaces.push({ kind: SurfaceParking, name: 'P1', points: [{ lat: 0, lon: 0 }] });
  assert.equal(hasParking(apt), true);
  assert.equal(parkingSurfaces(apt).length, 1);
});

test('updateAirportHeaders and resetDocument', () => {
  const doc = createEmptyDocument();
  updateAirportHeaders(doc, { icao: 'KBTV', fieldElev: 100 });
  assert.equal(doc.airport.icao, 'KBTV');
  assert.equal(doc.aptDirty, true);
  // surfaces not replaced
  addSurface(doc, { kind: SurfaceHold, name: 'H1', points: [{ lat: 1, lon: 1 }] });
  updateAirportHeaders(doc, { surfaces: [] });
  assert.equal(doc.airport.surfaces.length, 1);

  resetDocument(doc);
  assert.equal(doc.airport, null);
  assert.equal(doc.mode, MODE_SELECT);
});

test('minPointsForKind', () => {
  assert.equal(minPointsForKind(SurfaceParking), 1);
  assert.equal(minPointsForKind(SurfaceHold), 1);
  assert.equal(minPointsForKind(SurfaceTaxiway), 2);
  assert.equal(minPointsForKind(SurfaceRunway), 2);
});
