import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import {
  validateAPT,
  validateAIR,
  crossFileWarnings,
  validateDocument,
} from '../../internal/web/static/js/openfsd/airport-editor/validate.js';
import {
  createEmptyDocument,
  setAirport,
  setAircraft,
} from '../../internal/web/static/js/openfsd/airport-editor/model.js';

const __dirname = dirname(fileURLToPath(import.meta.url));
const fixtures = join(__dirname, '../../pkg/twrfiles/testdata');

test('validateAPT ok fixture', () => {
  const text = readFileSync(join(fixtures, 'KBTV_example.apt'), 'utf8');
  const r = validateAPT(text);
  assert.equal(r.ok, true);
  assert.equal(r.errors.length, 0);
  assert.equal(r.airport.icao, 'KBTV');
});

test('validateAPT errors', () => {
  const r = validateAPT('icao=BTV\n');
  assert.equal(r.ok, false);
  assert.ok(r.errors.some((e) => e.includes('ICAO')));
});

test('validateAIR ok fixture', () => {
  const text = readFileSync(join(fixtures, 'KBTV_example.air'), 'utf8');
  const r = validateAIR(text);
  assert.equal(r.ok, true);
  assert.equal(r.aircraft.length, 3);
});

test('validateAIR errors', () => {
  const r = validateAIR('bad\n');
  assert.equal(r.ok, false);
  assert.ok(r.errors.length >= 1);
});

test('crossFileWarnings dep mismatch and far aircraft', () => {
  const apt = {
    icao: 'KBTV',
    surfaces: [{ kind: 'PARKING', name: 'G1', points: [{ lat: 44.47, lon: -73.15 }] }],
  };
  const aircraft = [
    {
      callsign: 'A1',
      dep: 'KBOS',
      lat: 44.47,
      lon: -73.15,
    },
    {
      callsign: 'FAR1',
      dep: 'KBTV',
      lat: 0,
      lon: 0,
    },
  ];
  const warns = crossFileWarnings(apt, aircraft);
  assert.ok(warns.some((w) => w.includes('dep KBOS')));
  assert.ok(warns.some((w) => w.includes('FAR1') && w.includes('NM')));
});

test('crossFileWarnings aircraft without airport', () => {
  const warns = crossFileWarnings(null, [{ callsign: 'X', dep: 'KBTV', lat: 1, lon: 2 }]);
  assert.ok(warns.some((w) => w.includes('without airport')));
});

test('validateDocument soft warnings', () => {
  const doc = createEmptyDocument();
  setAirport(doc, {
    icao: 'KBTV',
    surfaces: [{ kind: 'PARKING', name: 'G1', points: [{ lat: 44.47, lon: -73.15 }] }],
  });
  setAircraft(doc, [{ callsign: 'A1', dep: 'KBOS', lat: 44.47, lon: -73.15 }]);
  const r = validateDocument(doc);
  assert.ok(r.softWarnings.some((w) => w.includes('dep')));
  assert.deepEqual(doc.softWarnings, r.softWarnings);
});

test('crossFileWarnings anchor via runway when no parking', () => {
  const apt = {
    icao: 'KBTV',
    surfaces: [
      {
        kind: 'RUNWAY',
        name: '1/19',
        points: [
          { lat: 44.47, lon: -73.15 },
          { lat: 44.48, lon: -73.16 },
        ],
      },
    ],
  };
  const warns = crossFileWarnings(apt, [{ callsign: 'FAR1', dep: 'KBTV', lat: 0, lon: 0 }]);
  assert.ok(warns.some((w) => w.includes('FAR1')));
});
