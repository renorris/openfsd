import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import {
  parseAIR,
  isSquawk,
} from '../../internal/web/static/js/openfsd/airport-editor/parse-air.js';
import {
  EngineJet,
  EngineTurboprop,
  EnginePiston,
  RulesIFR,
  RulesVFR,
  XPDRModeStandby,
  XPDRModeNormal,
} from '../../internal/web/static/js/openfsd/airport-editor/model.js';

const __dirname = dirname(fileURLToPath(import.meta.url));
const fixtures = join(__dirname, '../../pkg/twrfiles/testdata');

test('parseAIR KBTV fixture', () => {
  const data = readFileSync(join(fixtures, 'KBTV_example.air'), 'utf8');
  const { aircraft, errors } = parseAIR(data);
  assert.equal(errors.length, 0);
  assert.equal(aircraft.length, 3);

  const a = aircraft[0];
  assert.equal(a.callsign, 'AAL123');
  assert.equal(a.type, 'B738/F');
  assert.equal(a.engine, EngineJet);
  assert.equal(a.rules, RulesIFR);
  assert.equal(a.dep, 'KBTV');
  assert.equal(a.arr, 'KBOS');
  assert.equal(a.cruiseAlt, 29000);
  assert.equal(a.route, 'BTV4 MPV LEB MHT');
  assert.equal(a.remarks, '/v/charts');
  assert.equal(a.squawk, '2200');
  assert.equal(a.xpdrMode, XPDRModeStandby);
  assert.equal(a.lat, 44.469758);
  assert.equal(a.lon, -73.154747);
  assert.equal(a.alt, 335);
  assert.equal(a.speed, 0);
  assert.equal(a.heading, 360);

  assert.equal(aircraft[1].callsign, 'USA456');
  assert.equal(aircraft[1].engine, EngineTurboprop);
  assert.equal(aircraft[2].callsign, 'N4729H');
  assert.equal(aircraft[2].engine, EnginePiston);
  assert.equal(aircraft[2].rules, RulesVFR);
  assert.equal(aircraft[2].squawk, '1200');
});

test('parseAIR comments blank CRLF', () => {
  const text =
    '; header\r\n\r\nAAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90\r\n';
  const { aircraft, errors } = parseAIR(text);
  assert.equal(errors.length, 0);
  assert.equal(aircraft.length, 1);
  assert.equal(aircraft[0].callsign, 'AAL1');
  assert.equal(aircraft[0].xpdrMode, XPDRModeNormal);
});

test('parseAIR too few fields', () => {
  const { errors } = parseAIR('AAL1:B738:J:I:KBTV\n');
  assert.equal(errors.length, 1);
  assert.ok(errors[0].includes('Invalid number of fields'));
});

test('parseAIR duplicate callsign', () => {
  const line = 'AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90';
  const { aircraft, errors } = parseAIR(`${line}\n${line}\n`);
  assert.equal(aircraft.length, 1);
  assert.equal(errors.length, 1);
  assert.ok(errors[0].includes('Duplicate callsign'));
});

test('parseAIR invalid engine rules xpdr', () => {
  let r = parseAIR('AAL1:B738:X:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90\n');
  assert.ok(r.errors[0].includes('engine type'));
  r = parseAIR('AAL1:B738:J:Z:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90\n');
  assert.ok(r.errors[0].includes('flight plan type'));
  r = parseAIR('AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:X:44.0:-73.0:335:0:90\n');
  assert.ok(r.errors[0].includes('transponder mode'));
});

test('parseAIR invalid numeric fields', () => {
  const lines = [
    'AAL1:B738:J:I:KBTV:KBOS:abc:DCT::2200:N:44.0:-73.0:335:0:90',
    'AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:xx:-73.0:335:0:90',
    'AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:yy:335:0:90',
    'AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:zz:0:90',
    'AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:no:90',
    'AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:no',
  ];
  for (const line of lines) {
    const { errors } = parseAIR(`${line}\n`);
    assert.equal(errors.length, 1, line);
    assert.ok(errors[0].includes('Invalid numeric field'), line);
  }
});

test('parseAIR missing callsign', () => {
  const { errors } = parseAIR(':B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90\n');
  assert.equal(errors.length, 1);
  assert.ok(errors[0].includes('Missing callsign'));
});

test('parseAIR all engines and rules case fold', () => {
  const mk = (cs, eng, rules) =>
    `${cs}:C172:${eng}:${rules}:KBTV:KBOS:5000:DCT::1200:N:44.0:-73.0:335:0:90`;
  const text = [mk('A1', 'P', 'V'), mk('A2', 'T', 'I'), mk('A3', 'J', 'D'), mk('A4', 'H', 'S'), mk('a5', 'p', 'v')].join(
    '\n',
  );
  const { aircraft, errors } = parseAIR(text);
  assert.equal(errors.length, 0);
  assert.equal(aircraft.length, 5);
  assert.equal(aircraft[4].callsign, 'A5');
  assert.equal(aircraft[4].engine, 'P');
  assert.equal(aircraft[4].rules, 'V');
});

test('parseAIR best-effort partial load', () => {
  const text = [
    'GOOD1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90',
    'BAD:too:few',
    'GOOD2:C172:P:V:KBTV:KLEB:5000:DCT::1200:S:44.1:-73.1:335:0:180',
    'GOOD1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90',
  ].join('\n');
  const { aircraft, errors } = parseAIR(text);
  assert.equal(aircraft.length, 2);
  assert.equal(errors.length, 2);
  assert.equal(aircraft[0].callsign, 'GOOD1');
  assert.equal(aircraft[1].callsign, 'GOOD2');
});

test('parseAIR cruise alt float trunc', () => {
  const { aircraft, errors } = parseAIR(
    'AAL1:B738:J:I:KBTV:KBOS:29000.9:DCT::2200:N:44.0:-73.0:335:0:90\n',
  );
  assert.equal(errors.length, 0);
  assert.equal(aircraft[0].cruiseAlt, 29000);
});

test('parseAIR extra fields ignored', () => {
  const { aircraft, errors } = parseAIR(
    'AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90:EXTRA:MORE\n',
  );
  assert.equal(errors.length, 0);
  assert.equal(aircraft[0].heading, 90);
});

test('parseAIR invalid squawk', () => {
  const mk = (cs, sqk) =>
    `${cs}:B738:J:I:KBTV:KBOS:10000:DCT::${sqk}:N:44.0:-73.0:335:0:90\n`;
  for (const sqk of ['', '12', '123', '12345', '12A0', 'ABCD', '  ']) {
    const { aircraft, errors } = parseAIR(mk('AAL1', sqk));
    assert.equal(aircraft.length, 0, sqk);
    assert.ok(errors[0].includes('Invalid squawk code'), sqk);
  }
  const { aircraft, errors } = parseAIR(mk('AAL1', '1200') + mk('AAL2', '7700'));
  assert.equal(errors.length, 0);
  assert.equal(aircraft.length, 2);
});

test('isSquawk', () => {
  assert.equal(isSquawk('1200'), true);
  assert.equal(isSquawk('0000'), true);
  assert.equal(isSquawk('12'), false);
  assert.equal(isSquawk('12A0'), false);
});
