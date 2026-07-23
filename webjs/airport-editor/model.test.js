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
  SurfaceParking,
  SurfaceRunway,
  DefaultRegistration,
  DEFAULT_PATTERN_SIZE,
} from '../../internal/web/static/js/openfsd/airport-editor/model.js';

test('createEmptyDocument', () => {
  const doc = createEmptyDocument();
  assert.equal(doc.airport, null);
  assert.deepEqual(doc.aircraft, []);
  assert.equal(doc.aptDirty, false);
  assert.equal(doc.airDirty, false);
  assert.equal(doc.mode, 'select');
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
