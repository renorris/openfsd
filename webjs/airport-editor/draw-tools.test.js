import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  createDrawSession,
  beginDraw,
  cancelDraw,
  addDrawPoint,
  canFinishDraw,
  finishMinPoints,
  buildParkingSurface,
  buildPathSurface,
  buildRunwaySurface,
  handleMapClick,
  finishDraw,
  escapeDraw,
  isPolylineMode,
  isClickPlaceMode,
  modeToKind,
} from '../../internal/web/static/js/openfsd/airport-editor/draw-tools.js';
import {
  createEmptyDocument,
  MODE_PARK,
  MODE_TAXI,
  MODE_RUNWAY,
  MODE_HOLD,
  MODE_AIRCRAFT,
  MODE_SELECT,
  SurfaceTaxiway,
  SurfaceHold,
  SurfaceRunway,
  SurfaceParking,
} from '../../internal/web/static/js/openfsd/airport-editor/model.js';

test('mode helpers', () => {
  assert.equal(isPolylineMode(MODE_TAXI), true);
  assert.equal(isPolylineMode(MODE_PARK), false);
  assert.equal(isClickPlaceMode(MODE_PARK), true);
  assert.equal(isClickPlaceMode(MODE_AIRCRAFT), true);
  assert.equal(modeToKind(MODE_RUNWAY), SurfaceRunway);
  assert.equal(modeToKind(MODE_SELECT), null);
  assert.equal(finishMinPoints(SurfaceHold), 1);
  assert.equal(finishMinPoints(SurfaceTaxiway), 2);
});

test('draw session begin/cancel/add', () => {
  const s = createDrawSession();
  beginDraw(s, MODE_TAXI);
  assert.equal(s.active, true);
  assert.equal(s.kind, SurfaceTaxiway);
  addDrawPoint(s, { lat: 1, lon: 2 });
  assert.equal(s.points.length, 1);
  assert.equal(canFinishDraw(s), false);
  addDrawPoint(s, { lat: 3, lon: 4 });
  assert.equal(canFinishDraw(s), true);
  assert.equal(cancelDraw(s), true);
  assert.equal(s.active, false);
  assert.equal(s.points.length, 0);
});

test('buildParkingSurface', () => {
  const ok = buildParkingSurface('G1', { lat: 1, lon: 2 });
  assert.equal(ok.ok, true);
  assert.equal(ok.surface.kind, SurfaceParking);
  assert.equal(ok.surface.name, 'G1');

  const bad = buildParkingSurface('bad-name', { lat: 1, lon: 2 });
  assert.equal(bad.ok, false);
});

test('buildPathSurface and runway', () => {
  const taxi = buildPathSurface(SurfaceTaxiway, 'A', [
    { lat: 1, lon: 1 },
    { lat: 2, lon: 2 },
  ]);
  assert.equal(taxi.ok, true);
  assert.equal(taxi.surface.name, 'A');

  const short = buildPathSurface(SurfaceTaxiway, 'A', [{ lat: 1, lon: 1 }]);
  assert.equal(short.ok, false);

  const rwy = buildRunwaySurface('19', '1', [
    { lat: 1, lon: 1 },
    { lat: 2, lon: 2 },
  ]);
  assert.equal(rwy.ok, true);
  assert.equal(rwy.surface.name, '19/1');
  assert.equal(rwy.surface.rwyA, '19');

  const badRwy = buildRunwaySurface('04', '22', [
    { lat: 1, lon: 1 },
    { lat: 2, lon: 2 },
  ]);
  assert.equal(badRwy.ok, false);
});

test('handleMapClick park with prompt', () => {
  const doc = createEmptyDocument();
  doc.mode = MODE_PARK;
  const session = createDrawSession();
  const res = handleMapClick(doc, session, { lat: 44, lon: -73 }, {
    prompt: () => 'P9',
  });
  assert.equal(res.action, 'placed-park');
  assert.equal(doc.airport.surfaces.length, 1);
  assert.equal(doc.airport.surfaces[0].name, 'P9');
  assert.equal(doc.aptDirty, true);
});

test('handleMapClick aircraft defaults ICAO', () => {
  const doc = createEmptyDocument();
  doc.mode = MODE_AIRCRAFT;
  // seed airport ICAO
  handleMapClick(
    (() => {
      doc.mode = MODE_PARK;
      handleMapClick(doc, createDrawSession(), { lat: 0, lon: 0 }, { prompt: () => 'P1' });
      doc.airport.icao = 'KBTV';
      doc.airport.fieldElev = 100;
      doc.mode = MODE_AIRCRAFT;
      return doc;
    })(),
    createDrawSession(),
    { lat: 1, lon: 2 },
  );
  assert.equal(doc.aircraft.length, 1);
  assert.equal(doc.aircraft[0].dep, 'KBTV');
  assert.equal(doc.aircraft[0].arr, 'KBTV');
  assert.equal(doc.aircraft[0].alt, 100);
});

test('polyline finishDraw', () => {
  const doc = createEmptyDocument();
  doc.mode = MODE_TAXI;
  const session = createDrawSession();
  beginDraw(session, MODE_TAXI);
  addDrawPoint(session, { lat: 1, lon: 1 });
  addDrawPoint(session, { lat: 2, lon: 2 });
  const res = finishDraw(doc, session, { prompt: () => 'A' });
  assert.equal(res.action, 'finished');
  assert.equal(doc.airport.surfaces[0].kind, SurfaceTaxiway);
  assert.equal(doc.mode, MODE_SELECT);
  assert.equal(session.active, false);
});

test('finishDraw runway prompts both ends', () => {
  const doc = createEmptyDocument();
  doc.mode = MODE_RUNWAY;
  const session = createDrawSession();
  beginDraw(session, MODE_RUNWAY);
  addDrawPoint(session, { lat: 1, lon: 1 });
  addDrawPoint(session, { lat: 2, lon: 2 });
  let n = 0;
  const res = finishDraw(doc, session, {
    prompt: () => {
      n++;
      return n === 1 ? '15' : '33';
    },
  });
  assert.equal(res.action, 'finished');
  assert.equal(doc.airport.surfaces[0].name, '15/33');
});

test('escapeDraw returns to select', () => {
  const doc = createEmptyDocument();
  doc.mode = MODE_TAXI;
  const session = createDrawSession();
  beginDraw(session, MODE_TAXI);
  addDrawPoint(session, { lat: 1, lon: 1 });
  assert.equal(escapeDraw(doc, session), true);
  assert.equal(doc.mode, MODE_SELECT);
  assert.equal(session.active, false);
});

test('handleMapClick taxi accumulates vertices', () => {
  const doc = createEmptyDocument();
  doc.mode = MODE_TAXI;
  const session = createDrawSession();
  let r = handleMapClick(doc, session, { lat: 1, lon: 1 });
  assert.equal(r.action, 'need-more');
  r = handleMapClick(doc, session, { lat: 2, lon: 2 });
  assert.equal(r.action, 'vertex');
  assert.equal(session.points.length, 2);
});
