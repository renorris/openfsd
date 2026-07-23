import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import {
  parseAPT,
  isWordName,
  isTaxiHoldName,
  isRunwayDesignator,
} from '../../internal/web/static/js/openfsd/airport-editor/parse-apt.js';
import {
  SurfaceParking,
  SurfaceRunway,
  SurfaceTaxiway,
  findSurface,
} from '../../internal/web/static/js/openfsd/airport-editor/model.js';

const __dirname = dirname(fileURLToPath(import.meta.url));
const fixtures = join(__dirname, '../../pkg/twrfiles/testdata');

test('parseAPT KBTV fixture', () => {
  const data = readFileSync(join(fixtures, 'KBTV_example.apt'), 'utf8');
  const { airport, errors } = parseAPT(data);
  assert.equal(errors.length, 0);
  assert.equal(airport.icao, 'KBTV');
  assert.equal(airport.magVar, 16);
  assert.equal(airport.fieldElev, 335);
  assert.equal(airport.patternElev, 1335);
  assert.equal(airport.patternSize, 1);
  assert.equal(airport.initClimbProps, 10000);
  assert.equal(airport.initClimbJets, 10000);
  assert.equal(airport.registration, 'N');
  assert.ok(airport.jetAirlines.includes('AAL'));
  assert.ok(airport.turboAirlines.includes('EGF'));

  let nPark = 0;
  let nRwy = 0;
  let nTaxi = 0;
  let nHold = 0;
  for (const s of airport.surfaces) {
    switch (s.kind) {
      case SurfaceParking:
        nPark++;
        assert.equal(s.points.length, 1);
        break;
      case SurfaceRunway:
        nRwy++;
        assert.ok(s.points.length >= 2);
        break;
      case SurfaceTaxiway:
        nTaxi++;
        break;
      default:
        nHold++;
    }
  }
  assert.equal(nPark, 16);
  assert.equal(nRwy, 2);
  assert.equal(nTaxi, 12);
  assert.equal(nHold, 0);

  const rwy = findSurface(airport, '19');
  assert.ok(rwy);
  assert.equal(rwy.name, '19/1');
  assert.equal(rwy.dispA, 0);
  assert.equal(rwy.dispB, 225);
  assert.equal(rwy.turnoffLeft, false);

  const rwy2 = findSurface(airport, '33/15');
  assert.ok(rwy2);
  assert.equal(rwy2.dispA, 500);
  assert.equal(rwy2.dispB, 0);
  assert.equal(rwy2.turnoffLeft, true);

  const g1 = findSurface(airport, 'g1');
  assert.ok(g1);
  assert.equal(g1.kind, SurfaceParking);
});

test('parseAPT defaults', () => {
  const { airport, errors } = parseAPT('icao=KXYZ\n');
  assert.equal(errors.length, 0);
  assert.equal(airport.patternSize, 1);
  assert.equal(airport.initClimbProps, 3000);
  assert.equal(airport.initClimbJets, 5000);
  assert.equal(airport.registration, 'N');
});

test('parseAPT invalid ICAO', () => {
  const { errors } = parseAPT('icao=BTV\n');
  assert.ok(errors.length >= 1);
  assert.ok(errors[0].includes('ICAO'));
});

test('parseAPT comments blank CRLF', () => {
  const text = '; comment\r\n\r\nicao=KBTV\r\nmagnetic variation=16\r\n';
  const { airport, errors } = parseAPT(text);
  assert.equal(errors.length, 0);
  assert.equal(airport.icao, 'KBTV');
  assert.equal(airport.magVar, 16);
});

test('parseAPT invalid numeric headers', () => {
  const text = [
    'icao=KBTV',
    'magnetic variation=abc',
    'field elevation=xyz',
    'pattern elevation=?',
    'pattern size=nope',
    'initial climb props=bad',
    'initial climb jets=bad',
  ].join('\n');
  const { errors } = parseAPT(text);
  assert.ok(errors.length >= 6);
});

test('parseAPT parking waypoint counts', () => {
  const text = `
icao=KBTV
[PARKING G1]
; no waypoint
[PARKING G2]
44.1 -73.1
44.2 -73.2
`;
  const { errors } = parseAPT(text);
  assert.ok(errors.some((e) => e.includes('has no waypoint defined')));
  assert.ok(errors.some((e) => e.includes('Extra waypoint found in parking')));
});

test('parseAPT runway validation', () => {
  const text = `
icao=KBTV
[RUNWAY 19/1]
44.1 -73.1
[RUNWAY 99/1]
[RUNWAY 22/04]
[RUNWAY 15L/33R]
44.0 -73.0
44.1 -73.1
`;
  const { airport, errors } = parseAPT(text);
  assert.ok(errors.some((e) => e.includes('Runway 19/1')));
  assert.ok(errors.some((e) => e.includes('Unknown line')));
  assert.ok(findSurface(airport, '15L'));
});

test('parseAPT taxiway and hold', () => {
  const text = `
icao=KBTV
[TAXIWAY A]
44.1 -73.1
[TAXIWAY B1]
44.0 -73.0
44.1 -73.1
[HOLD HS1]
44.5 -73.5
[HOLD HS2]
[HOLD HS3]
44.0 -73.0
44.1 -73.1
[TAXIWAY 1BAD]
[HOLD bad-name]
`;
  const { airport, errors } = parseAPT(text);
  assert.ok(errors.some((e) => e.includes('Taxiway A')));
  assert.ok(errors.some((e) => e.includes('Hold HS2 has no waypoint')));
  assert.ok(errors.some((e) => e.includes('Extra waypoint found in hold section HS3')));
  assert.ok(errors.filter((e) => e.includes('Unknown line')).length >= 2);
  assert.ok(findSurface(airport, 'B1'));
  assert.ok(findSurface(airport, 'HS1'));
});

test('parseAPT duplicates', () => {
  const text = `
icao=KBTV
[PARKING G1]
44.1 -73.1
[PARKING G1]
44.2 -73.2
[TAXIWAY A]
44.0 -73.0
44.1 -73.1
[TAXIWAY A]
44.0 -73.0
44.1 -73.1
[RUNWAY 1/19]
44.0 -73.0
44.1 -73.1
[RUNWAY 1/19]
44.0 -73.0
44.1 -73.1
[HOLD H1]
44.5 -73.5
[HOLD H1]
44.6 -73.6
`;
  const { errors } = parseAPT(text);
  assert.equal(errors.filter((e) => e.includes('Duplicate')).length, 4);
});

test('parseAPT taxi/hold shared namespace', () => {
  const text = `
icao=KBTV
[TAXIWAY A]
44.0 -73.0
44.1 -73.1
[HOLD A]
44.5 -73.5
[HOLD B]
44.6 -73.6
[TAXIWAY B]
44.0 -73.0
44.1 -73.1
`;
  const { errors } = parseAPT(text);
  assert.equal(errors.filter((e) => e.includes('Duplicate taxiway or hold')).length, 2);
});

test('parseAPT registration and airline lists', () => {
  const cases = [
    { text: 'icao=KBTV\nregistration=N123\n', want: 'Invalid registration prefix' },
    { text: 'icao=KBTV\nregistration=\n', want: 'Invalid registration prefix' },
    {
      text: 'icao=KBTV\njet airlines=TOOLONGPREFIX,AAL\n',
      want: 'Invalid list of jet airlines',
    },
    { text: 'icao=KBTV\njet airlines=A,AAL\n', want: 'Invalid list of jet airlines' },
    {
      text: 'icao=KBTV\nturboprop airlines=EGF,,USA\n',
      want: 'Invalid list of turboprop airlines',
    },
  ];
  for (const c of cases) {
    const { errors } = parseAPT(c.text);
    assert.ok(
      errors.some((e) => e.includes(c.want)),
      `expected ${c.want} in ${JSON.stringify(errors)}`,
    );
  }
});

test('parseAPT turnoff invalid and displaced outside runway', () => {
  const text = `
icao=KBTV
displaced threshold=10/20
[RUNWAY 1/19]
turnoff=up
44.0 -73.0
44.1 -73.1
`;
  const { errors } = parseAPT(text);
  assert.ok(errors.some((e) => e.includes('Unknown line')));
  assert.ok(errors.some((e) => e.includes('Invalid turnoff direction')));
});

test('validators isWordName isTaxiHoldName isRunwayDesignator', () => {
  assert.equal(isWordName('G1'), true);
  assert.equal(isWordName(''), false);
  assert.equal(isWordName('bad-name'), false);
  assert.equal(isTaxiHoldName('A'), true);
  assert.equal(isTaxiHoldName('B1'), true);
  assert.equal(isTaxiHoldName('1BAD'), false);
  assert.equal(isTaxiHoldName('bad-name'), false);
  assert.equal(isRunwayDesignator('15L'), true);
  assert.equal(isRunwayDesignator('36'), true);
  assert.equal(isRunwayDesignator('99'), false);
  assert.equal(isRunwayDesignator('04'), false);
  assert.equal(isRunwayDesignator(''), false);
});

test('parseAPT point without current surface', () => {
  const { errors } = parseAPT('icao=KBTV\n44.1 -73.1\n');
  assert.ok(errors.some((e) => e.includes('Unknown line format found on line 2')));
});

test('parseAPT trailing airline comma allowed', () => {
  const { airport, errors } = parseAPT('icao=KBTV\njet airlines=AAL,ACA,\n');
  assert.equal(errors.length, 0);
  assert.equal(airport.jetAirlines, 'AAL,ACA,');
});

test('parseAPT invalid parking name', () => {
  const { errors } = parseAPT('icao=KBTV\n[PARKING bad-name]\n44.1 -73.1\n');
  assert.ok(errors.some((e) => e.includes('Unknown line')));
});
