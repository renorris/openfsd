import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { parseAIR } from '../../internal/web/static/js/openfsd/airport-editor/parse-air.js';
import { formatAIR } from '../../internal/web/static/js/openfsd/airport-editor/format-air.js';
import {
  EnginePiston,
  EngineJet,
  RulesVFR,
  RulesIFR,
  XPDRModeNormal,
  XPDRModeStandby,
} from '../../internal/web/static/js/openfsd/airport-editor/model.js';

const __dirname = dirname(fileURLToPath(import.meta.url));
const fixtures = join(__dirname, '../../pkg/twrfiles/testdata');

test('formatAIR KBTV golden byte-equal', () => {
  const raw = readFileSync(join(fixtures, 'KBTV_example.air'), 'utf8');
  const { aircraft, errors } = parseAIR(raw);
  assert.equal(errors.length, 0);
  const got = formatAIR(aircraft);
  const want = readFileSync(join(fixtures, 'KBTV_example.formatted.air'), 'utf8');
  assert.equal(got, want);
  assert.ok(!got.includes('\r'));
  assert.ok(got.endsWith('\n'));
  assert.ok(!got.includes(';'));
});

test('formatAIR round-trip structural + idempotent', () => {
  const raw = readFileSync(join(fixtures, 'KBTV_example.air'), 'utf8');
  const { aircraft: r1, errors: e1 } = parseAIR(raw);
  assert.equal(e1.length, 0);
  const text = formatAIR(r1);
  const { aircraft: r2, errors: e2 } = parseAIR(text);
  assert.equal(e2.length, 0);
  assert.deepEqual(r1, r2);
  assert.equal(formatAIR(r2), text);
});

test('formatAIR empty list', () => {
  assert.equal(formatAIR(null), '');
  assert.equal(formatAIR([]), '');
});

test('formatAIR sixteen fields and uppercase', () => {
  const rows = [
    {
      callsign: 'aal99',
      type: 'b738/f',
      engine: 'j',
      rules: 'i',
      dep: 'kbtv',
      arr: 'kbos',
      cruiseAlt: 29000,
      route: 'BTV4 MPV',
      remarks: '/v/charts',
      squawk: '2200',
      xpdrMode: 's',
      lat: 44.469758,
      lon: -73.154747,
      alt: 335,
      speed: 0,
      heading: 360,
    },
  ];
  const got = formatAIR(rows);
  const line = got.replace(/\n$/, '');
  const fields = line.split(':');
  assert.equal(fields.length, 16);
  assert.equal(fields[0], 'AAL99');
  assert.equal(fields[1], 'B738/F');
  assert.equal(fields[2], 'J');
  assert.equal(fields[3], 'I');
  assert.equal(fields[4], 'KBTV');
  assert.equal(fields[5], 'KBOS');
  assert.equal(fields[6], '29000');
  assert.equal(fields[7], 'BTV4 MPV');
  assert.equal(fields[8], '/v/charts');
  assert.equal(fields[10], 'S');
  assert.equal(fields[11], '44.469758');
  assert.equal(fields[12], '-73.154747');
  assert.equal(fields[13], '335');
  assert.equal(fields[14], '0');
  assert.equal(fields[15], '360');
});

test('formatAIR compact vs fractional', () => {
  const rows = [
    {
      callsign: 'N1',
      type: 'C172',
      engine: EnginePiston,
      rules: RulesVFR,
      dep: 'KBTV',
      arr: 'KLEB',
      cruiseAlt: 3500,
      route: 'DCT',
      remarks: '',
      squawk: '1200',
      xpdrMode: XPDRModeNormal,
      lat: 44.5,
      lon: -73.1,
      alt: 335.5,
      speed: 90.25,
      heading: 180.0,
    },
  ];
  const got = formatAIR(rows);
  const fields = got.replace(/\n$/, '').split(':');
  assert.equal(fields.length, 16);
  assert.equal(fields[11], '44.500000');
  assert.equal(fields[12], '-73.100000');
  assert.equal(fields[13], '335.5');
  assert.equal(fields[14], '90.25');
  assert.equal(fields[15], '180');
  assert.equal(fields[8], '');
});

test('formatAIR preserves route/remarks as stored', () => {
  const rows = [
    {
      callsign: 'X1',
      type: 'A320',
      engine: EngineJet,
      rules: RulesIFR,
      dep: 'KBTV',
      arr: 'KBOS',
      cruiseAlt: 10000,
      route: 'mixed Case Route',
      remarks: 'keep /v mixed',
      squawk: '1234',
      xpdrMode: XPDRModeStandby,
      lat: 1,
      lon: 2,
      alt: 3,
      speed: 4,
      heading: 5,
    },
  ];
  const got = formatAIR(rows);
  assert.ok(got.includes(':mixed Case Route:keep /v mixed:'));
});

test('formatAirCompactFloat non-finite falls back', async () => {
  const { formatAirCompactFloat } = await import(
    '../../internal/web/static/js/openfsd/airport-editor/format-air.js'
  );
  assert.equal(formatAirCompactFloat(Number.NaN), '0');
  assert.equal(formatAirCompactFloat(Number.POSITIVE_INFINITY), '0');
});
