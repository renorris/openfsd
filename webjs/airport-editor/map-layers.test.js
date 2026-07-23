import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  surfaceStyle,
  collectLatLngs,
  countSurfacesByKind,
  titlebarChips,
  normalizeSelection,
  surfaceLabel,
  kindShort,
  OSM_TILE_URL,
  OSM_ATTRIBUTION,
  ESRI_TILE_URL,
  ESRI_ATTRIBUTION,
} from '../../internal/web/static/js/openfsd/airport-editor/map-layers.js';
import {
  createEmptyDocument,
  SurfaceParking,
  SurfaceRunway,
  SurfaceTaxiway,
  SurfaceHold,
} from '../../internal/web/static/js/openfsd/airport-editor/model.js';

test('surfaceStyle differs by kind and selection', () => {
  const taxi = surfaceStyle(SurfaceTaxiway, false);
  const taxiSel = surfaceStyle(SurfaceTaxiway, true);
  assert.ok(taxi.weight >= 2);
  assert.notEqual(taxi.color, taxiSel.color);
  const rwy = surfaceStyle(SurfaceRunway, false);
  assert.ok(rwy.weight >= taxi.weight);
  const park = surfaceStyle(SurfaceParking, false);
  assert.ok(park.radius >= 4);
  const hold = surfaceStyle(SurfaceHold, false);
  assert.ok(hold.dashArray);
});

test('collectLatLngs from surfaces and aircraft', () => {
  assert.deepEqual(collectLatLngs(null, null), []);
  const airport = {
    surfaces: [
      { kind: SurfaceParking, name: 'G1', points: [{ lat: 44.1, lon: -73.1 }] },
      {
        kind: SurfaceRunway,
        name: '19/1',
        points: [
          { lat: 44.2, lon: -73.2 },
          { lat: 44.3, lon: -73.3 },
        ],
      },
    ],
  };
  const ac = [{ callsign: 'A', lat: 44.4, lon: -73.4 }];
  const pts = collectLatLngs(airport, ac);
  assert.equal(pts.length, 4);
  assert.deepEqual(pts[0], [44.1, -73.1]);
  assert.deepEqual(pts[3], [44.4, -73.4]);
  // Non-finite skipped
  assert.equal(
    collectLatLngs(
      { surfaces: [{ kind: SurfaceParking, name: 'X', points: [{ lat: NaN, lon: 1 }] }] },
      [{ lat: 1, lon: Infinity }],
    ).length,
    0,
  );
});

test('countSurfacesByKind', () => {
  assert.deepEqual(countSurfacesByKind(null), {
    park: 0,
    rwy: 0,
    taxi: 0,
    hold: 0,
    total: 0,
  });
  const c = countSurfacesByKind([
    { kind: SurfaceParking },
    { kind: SurfaceParking },
    { kind: SurfaceRunway },
    { kind: SurfaceTaxiway },
    { kind: SurfaceHold },
  ]);
  assert.equal(c.park, 2);
  assert.equal(c.rwy, 1);
  assert.equal(c.taxi, 1);
  assert.equal(c.hold, 1);
  assert.equal(c.total, 5);
});

test('titlebarChips', () => {
  const doc = createEmptyDocument();
  let chips = titlebarChips(doc);
  assert.equal(chips.icao, '—');
  assert.equal(chips.apt, 'APT');
  assert.equal(chips.air, 'AIR');
  assert.equal(chips.counts, '');

  doc.airport = {
    icao: 'kbtv',
    surfaces: [
      { kind: SurfaceParking, name: 'G1', points: [] },
      { kind: SurfaceRunway, name: '19/1', points: [] },
    ],
  };
  doc.aircraft = [{ callsign: 'A' }, { callsign: 'B' }];
  doc.aptDirty = true;
  chips = titlebarChips(doc);
  assert.equal(chips.icao, 'KBTV');
  assert.equal(chips.apt, 'APT*');
  assert.equal(chips.air, 'AIR');
  assert.match(chips.counts, /1 park/);
  assert.match(chips.counts, /1 rwy/);
  assert.match(chips.counts, /2 ac/);
});

test('normalizeSelection', () => {
  assert.equal(normalizeSelection(null), null);
  assert.equal(normalizeSelection({ type: 'surface', index: -1 }), null);
  assert.deepEqual(normalizeSelection({ type: 'surface', index: 2 }), {
    type: 'surface',
    index: 2,
  });
  assert.deepEqual(normalizeSelection({ type: 'aircraft', index: 0 }), {
    type: 'aircraft',
    index: 0,
  });
  assert.equal(normalizeSelection({ type: 'vertex', index: 0 }), null);
});

test('surfaceLabel and kindShort', () => {
  assert.equal(kindShort(SurfaceTaxiway), 'taxi');
  assert.equal(kindShort(SurfaceParking), 'park');
  assert.match(
    surfaceLabel({ kind: SurfaceRunway, name: '19/1', rwyA: '19', rwyB: '1' }),
    /RWY/,
  );
  assert.match(surfaceLabel({ kind: SurfaceParking, name: 'G1' }), /G1/);
});

test('basemap attribution strings are non-empty', () => {
  assert.ok(OSM_TILE_URL.includes('openstreetmap'));
  assert.ok(OSM_ATTRIBUTION.length > 10);
  assert.ok(OSM_ATTRIBUTION.toLowerCase().includes('openstreetmap'));
  assert.ok(ESRI_TILE_URL.includes('arcgisonline'));
  assert.ok(ESRI_ATTRIBUTION.length > 10);
  assert.ok(ESRI_ATTRIBUTION.toLowerCase().includes('esri'));
});
