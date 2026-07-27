import { test } from 'node:test';
import assert from 'node:assert/strict';

/**
 * Pure OverlayController helpers.
 *
 * RC1 live path = applySurfaceLatLngs (polyline setLatLngs / point setLatLng).
 * RC2 no companion circle factory in exports — single divIcon via builders.
 * RC3 no CSS margin in JS positioning — iconSize/iconAnchor only.
 * K5 suppress ≠ block render: dragging:false + within suppressMs still allows full render by contract.
 */
import {
  surfaceStyle,
  collectLatLngs,
  countSurfacesByKind,
  titlebarChips,
  normalizeSelection,
  surfaceLabel,
  kindShort,
  escapeHtml,
  OSM_TILE_URL,
  OSM_ATTRIBUTION,
  ESRI_TILE_URL,
  ESRI_ATTRIBUTION,
  VERTEX_HANDLE_PX,
  VERTEX_HIT_PX,
  VERTEX_PANE,
  MAP_CLICK_SUPPRESS_MS,
  buildVertexHandleOptions,
  buildVertexHandleIconOptions,
  findNearestVertexPx,
  pointsToLatLngs,
  applySurfaceLatLngs,
  patchVertexPoints,
  shouldSuppressMapClick,
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
  assert.equal(chips.issues, '—');
  assert.equal(chips.issuesTone, 'empty');

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
  assert.equal(chips.issues, 'OK');
  assert.equal(chips.issuesTone, 'ok');

  doc.aptErrors = ['bad header'];
  doc.softWarnings = ['dep mismatch'];
  chips = titlebarChips(doc);
  assert.equal(chips.issuesTone, 'err');
  assert.match(chips.issues, /2 issue/);

  doc.aptErrors = [];
  chips = titlebarChips(doc);
  assert.equal(chips.issuesTone, 'warn');
  assert.match(chips.issues, /1 warn/);
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

test('escapeHtml neutralizes markup in freeform AIR-like strings', () => {
  assert.equal(escapeHtml(null), '');
  assert.equal(escapeHtml('plain'), 'plain');
  assert.equal(
    escapeHtml('<IMG SRC=X ONERROR=ALERT(1)>'),
    '&lt;IMG SRC=X ONERROR=ALERT(1)&gt;',
  );
  assert.equal(escapeHtml(`a&b<'">`), 'a&amp;b&lt;&#39;&quot;&gt;');
  // Surface-style label still escapes if ever routed through HTML
  assert.equal(escapeHtml('G1 (park)'), 'G1 (park)');
});

test('pointsToLatLngs: empty, skips non-finite, preserves order', () => {
  assert.deepEqual(pointsToLatLngs(null), []);
  assert.deepEqual(pointsToLatLngs(undefined), []);
  assert.deepEqual(pointsToLatLngs([]), []);
  assert.deepEqual(
    pointsToLatLngs([
      { lat: 1, lon: 2 },
      { lat: NaN, lon: 3 },
      { lat: 4, lon: Infinity },
      { lat: 5, lon: 6 },
    ]),
    [
      [1, 2],
      [5, 6],
    ],
  );
});

test('applySurfaceLatLngs: polyline, point, none branches', () => {
  const polyCalls = [];
  const poly = {
    setLatLngs(ll) {
      polyCalls.push(ll);
    },
  };
  const latlngs = [
    [10, 20],
    [11, 21],
  ];
  assert.equal(applySurfaceLatLngs(poly, latlngs), 'polyline');
  assert.deepEqual(polyCalls, [latlngs]);

  const pointCalls = [];
  const point = {
    setLatLng(ll) {
      pointCalls.push(ll);
    },
  };
  assert.equal(applySurfaceLatLngs(point, latlngs), 'point');
  assert.deepEqual(pointCalls, [[10, 20]]);

  assert.equal(applySurfaceLatLngs({}, latlngs), 'none');
  assert.equal(applySurfaceLatLngs(null, latlngs), 'none');
  assert.equal(applySurfaceLatLngs(poly, []), 'none');
  assert.equal(applySurfaceLatLngs(poly, null), 'none');
});

test('patchVertexPoints: replaces only index vi; does not mutate input', () => {
  const src = [
    { lat: 1, lon: 2 },
    { lat: 3, lon: 4 },
    { lat: 5, lon: 6 },
  ];
  const next = patchVertexPoints(src, 1, 30, 40);
  assert.deepEqual(next, [
    { lat: 1, lon: 2 },
    { lat: 30, lon: 40 },
    { lat: 5, lon: 6 },
  ]);
  // Input not mutated
  assert.deepEqual(src[1], { lat: 3, lon: 4 });
  assert.deepEqual(patchVertexPoints(null, 0, 1, 2), []);
  assert.deepEqual(patchVertexPoints([], 0, 1, 2), []);
});

test('shouldSuppressMapClick matrix (K5: suppress ≠ block render)', () => {
  const ms = MAP_CLICK_SUPPRESS_MS;
  assert.equal(ms, 250);

  // dragging true → suppress (render still forbidden by caller invariant while pointer down)
  assert.equal(
    shouldSuppressMapClick({ dragging: true, dragEndedAt: 0, suppressMs: ms }, 1000),
    true,
  );
  assert.equal(
    shouldSuppressMapClick(
      { dragging: true, dragEndedAt: 900, suppressMs: ms },
      1000,
    ),
    true,
  );

  // dragging false + within suppressMs after dragEndedAt → suppress click
  // (full render is still allowed by contract — separate from this helper)
  assert.equal(
    shouldSuppressMapClick(
      { dragging: false, dragEndedAt: 1000, suppressMs: ms },
      1000 + ms - 1,
    ),
    true,
  );

  // dragging false + after suppress window → do not suppress
  assert.equal(
    shouldSuppressMapClick(
      { dragging: false, dragEndedAt: 1000, suppressMs: ms },
      1000 + ms,
    ),
    false,
  );
  assert.equal(
    shouldSuppressMapClick(
      { dragging: false, dragEndedAt: 1000, suppressMs: ms },
      1000 + ms + 50,
    ),
    false,
  );

  // dragEndedAt 0 → never suppress when not dragging
  assert.equal(
    shouldSuppressMapClick({ dragging: false, dragEndedAt: 0, suppressMs: ms }, 5000),
    false,
  );
});

test('buildVertexHandleIconOptions: iconSize/iconAnchor symmetry', () => {
  const icon = buildVertexHandleIconOptions();
  assert.equal(VERTEX_HANDLE_PX, 18);
  assert.equal(icon.iconSize[0], VERTEX_HANDLE_PX);
  assert.equal(icon.iconSize[1], VERTEX_HANDLE_PX);
  assert.equal(icon.iconAnchor[0], VERTEX_HANDLE_PX / 2);
  assert.equal(icon.iconAnchor[1], VERTEX_HANDLE_PX / 2);
  assert.match(icon.className, /apted-vertex-handle/);
  assert.match(icon.className, /leaflet-div-icon/);
});

test('buildVertexHandleOptions: visual-only (capture-phase owns drag)', () => {
  const opts = buildVertexHandleOptions(2);
  // Handles are non-interactive visuals; grab is map capture + pixel hit-test.
  assert.equal(opts.draggable, false);
  assert.equal(opts.interactive, false);
  assert.equal(opts.autoPan, false);
  assert.equal(opts.keyboard, false);
  assert.equal(opts.bubblingMouseEvents, false);
  assert.ok(opts.zIndexOffset >= 2000);
  assert.equal(opts.pane, VERTEX_PANE);
  assert.equal(VERTEX_PANE, 'aptedVertex');
  assert.match(opts.title, /Vertex 3/);
});

test('findNearestVertexPx: hit-test matrix (RCA capture-phase grab)', () => {
  assert.ok(VERTEX_HIT_PX >= 12);
  const verts = [
    { x: 100, y: 100 },
    { x: 200, y: 100 },
    { x: 200, y: 200 },
  ];
  // Exact hit
  assert.deepEqual(findNearestVertexPx(verts, { x: 100, y: 100 }, 16), {
    index: 0,
    dist: 0,
  });
  // Within radius of v1
  const near1 = findNearestVertexPx(verts, { x: 205, y: 103 }, 16);
  assert.ok(near1);
  assert.equal(near1.index, 1);
  // Outside all radii
  assert.equal(findNearestVertexPx(verts, { x: 0, y: 0 }, 16), null);
  // Prefer closer of two (equidistant uses last ≤ best — v0 at dist 10 wins first)
  const mid = findNearestVertexPx(
    [
      { x: 0, y: 0 },
      { x: 20, y: 0 },
    ],
    { x: 5, y: 0 },
    16,
  );
  assert.ok(mid);
  assert.equal(mid.index, 0);
  // Invalid inputs
  assert.equal(findNearestVertexPx(null, { x: 0, y: 0 }, 16), null);
  assert.equal(findNearestVertexPx(verts, null, 16), null);
  assert.equal(findNearestVertexPx(verts, { x: 100, y: 100 }, -1), null);
  // Skip non-finite vertices
  assert.equal(
    findNearestVertexPx([{ x: NaN, y: 0 }, { x: 10, y: 10 }], { x: 10, y: 10 }, 5)?.index,
    1,
  );
});
