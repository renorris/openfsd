import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { parseAPT } from '../../internal/web/static/js/openfsd/airport-editor/parse-apt.js';
import { formatAPT } from '../../internal/web/static/js/openfsd/airport-editor/format-apt.js';
import {
  SurfaceParking,
  SurfaceRunway,
  SurfaceTaxiway,
  SurfaceHold,
} from '../../internal/web/static/js/openfsd/airport-editor/model.js';

const __dirname = dirname(fileURLToPath(import.meta.url));
const fixtures = join(__dirname, '../../pkg/twrfiles/testdata');

test('formatAPT KBTV golden byte-equal', () => {
  const raw = readFileSync(join(fixtures, 'KBTV_example.apt'), 'utf8');
  const { airport, errors } = parseAPT(raw);
  assert.equal(errors.length, 0);
  const got = formatAPT(airport);
  const want = readFileSync(join(fixtures, 'KBTV_example.formatted.apt'), 'utf8');
  assert.equal(got, want);
  assert.ok(!got.includes('\r'));
  assert.ok(got.endsWith('\n'));
});

test('formatAPT round-trip structural + idempotent', () => {
  const raw = readFileSync(join(fixtures, 'KBTV_example.apt'), 'utf8');
  const { airport: a1, errors: e1 } = parseAPT(raw);
  assert.equal(e1.length, 0);
  const text = formatAPT(a1);
  const { airport: a2, errors: e2 } = parseAPT(text);
  assert.equal(e2.length, 0);
  assert.equal(a1.icao, a2.icao);
  assert.equal(a1.surfaces.length, a2.surfaces.length);
  for (let i = 0; i < a1.surfaces.length; i++) {
    assert.deepEqual(a1.surfaces[i], a2.surfaces[i]);
  }
  assert.equal(formatAPT(a2), text);
});

test('formatAPT empty airport defaults', () => {
  const got = formatAPT({});
  const want =
    'icao=\n' +
    'magnetic variation=0\n' +
    'field elevation=0\n' +
    'pattern elevation=0\n' +
    'pattern size=1\n' +
    'initial climb props=3000\n' +
    'initial climb jets=5000\n' +
    'jet airlines=\n' +
    'turboprop airlines=\n' +
    'registration=N\n' +
    '\n';
  assert.equal(got, want);
  const { airport, errors } = parseAPT(got);
  assert.equal(errors.length, 1);
  assert.ok(errors[0].includes('ICAO'));
  assert.equal(airport.patternSize, 1);
  assert.equal(airport.initClimbProps, 3000);
  assert.equal(airport.initClimbJets, 5000);
  assert.equal(airport.registration, 'N');
});

test('formatAPT parking only uppercases ICAO and coords', () => {
  const apt = {
    icao: 'kbtv',
    patternSize: 1,
    initClimbProps: 3000,
    initClimbJets: 5000,
    registration: 'N',
    surfaces: [
      {
        kind: SurfaceParking,
        name: 'G1',
        points: [{ lat: 44.46893, lon: -73.15392 }],
      },
    ],
  };
  const got = formatAPT(apt);
  assert.ok(got.includes('icao=KBTV\n'));
  assert.ok(got.includes('[PARKING G1]\n44.468930 -73.153920\n'));
  assert.ok(!got.includes('displaced'));
  assert.ok(!got.includes('turnoff'));
});

test('formatAPT runway turnoff left/right and order', () => {
  const apt = {
    icao: 'TEST',
    patternSize: 1,
    initClimbProps: 3000,
    initClimbJets: 5000,
    registration: 'N',
    surfaces: [
      {
        kind: SurfaceRunway,
        name: '19/1',
        rwyA: '19',
        rwyB: '1',
        dispA: 0,
        dispB: 0,
        turnoffLeft: true,
        points: [
          { lat: 1.0, lon: 2.0 },
          { lat: 3.0, lon: 4.0 },
        ],
      },
      {
        kind: SurfaceRunway,
        name: '10/28',
        rwyA: '10',
        rwyB: '28',
        dispA: 100,
        dispB: 200,
        turnoffLeft: false,
        points: [
          { lat: 5.5, lon: 6.5 },
          { lat: 7.25, lon: 8.75 },
        ],
      },
    ],
  };
  const got = formatAPT(apt);
  assert.ok(got.includes('[RUNWAY 19/1]\ndisplaced threshold=0/0\nturnoff=left\n'));
  assert.ok(got.includes('[RUNWAY 10/28]\ndisplaced threshold=100/200\nturnoff=right\n'));
  assert.ok(got.includes('1.000000 2.000000\n'));
  assert.ok(got.includes('7.250000 8.750000\n'));
  assert.ok(got.indexOf('[RUNWAY 19/1]') < got.indexOf('[RUNWAY 10/28]'));

  const { airport, errors } = parseAPT(got);
  assert.equal(errors.length, 0);
  assert.equal(airport.surfaces[0].turnoffLeft, true);
  assert.equal(airport.surfaces[1].turnoffLeft, false);
  assert.equal(airport.surfaces[1].dispA, 100);
  assert.equal(airport.surfaces[1].dispB, 200);
});

test('formatAPT surface order not rebucketed', () => {
  const apt = {
    icao: 'XXXX',
    patternSize: 1,
    initClimbProps: 3000,
    initClimbJets: 5000,
    registration: 'N',
    surfaces: [
      { kind: SurfaceTaxiway, name: 'A', points: [{ lat: 1.1, lon: 2.2 }, { lat: 3.3, lon: 4.4 }] },
      { kind: SurfaceParking, name: 'P1', points: [{ lat: 5.5, lon: 6.6 }] },
      { kind: SurfaceHold, name: 'H1', points: [{ lat: 7.7, lon: 8.8 }] },
      {
        kind: SurfaceRunway,
        name: '1/19',
        rwyA: '1',
        rwyB: '19',
        turnoffLeft: true,
        points: [
          { lat: 9.9, lon: 10.1 },
          { lat: 11.1, lon: 12.2 },
        ],
      },
    ],
  };
  const got = formatAPT(apt);
  const order = ['[TAXIWAY A]', '[PARKING P1]', '[HOLD H1]', '[RUNWAY 1/19]'];
  let prev = -1;
  for (const m of order) {
    const idx = got.indexOf(m);
    assert.ok(idx >= 0, m);
    assert.ok(idx > prev, m);
    prev = idx;
  }
});

test('formatAPT header float compact', () => {
  const apt = {
    icao: 'KXYZ',
    magVar: -14.5,
    fieldElev: 12.25,
    patternElev: 100.0,
    patternSize: 1.5,
    initClimbProps: 2500.5,
    initClimbJets: 4000,
    registration: 'C',
  };
  const got = formatAPT(apt);
  for (const line of [
    'magnetic variation=-14.5',
    'field elevation=12.25',
    'pattern elevation=100',
    'pattern size=1.5',
    'initial climb props=2500.5',
    'initial climb jets=4000',
    'registration=C',
  ]) {
    assert.ok(got.includes(`${line}\n`), line);
  }
});

test('formatAPT runway name from rwyA/rwyB and unknown kind', () => {
  const apt = {
    icao: 'XXXX',
    patternSize: 1,
    initClimbProps: 3000,
    initClimbJets: 5000,
    registration: 'N',
    surfaces: [
      {
        kind: SurfaceRunway,
        name: '',
        rwyA: '9',
        rwyB: '27',
        turnoffLeft: true,
        points: [
          { lat: 1, lon: 2 },
          { lat: 3, lon: 4 },
        ],
      },
      { kind: 'CUSTOM', name: 'Z', points: [{ lat: 5, lon: 6 }] },
    ],
  };
  const got = formatAPT(apt);
  assert.ok(got.includes('[RUNWAY 9/27]\n'));
  assert.ok(got.includes('[CUSTOM Z]\n'));
});
