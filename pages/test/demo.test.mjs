import test from 'node:test';
import assert from 'node:assert/strict';
import { assessRequest, files } from '../src/demo.mjs';

test('protected files keep a broad cleanup request denied', () => {
  assert.deepEqual(assessRequest(files), {
    result:'BLOCKED', decision:'DENY', checks:['BLOCKED','BLOCKED'], reason:'protected',
  });
});

test('scoping to observed temporary files satisfies both checks', () => {
  const result = assessRequest(['cache.tmp','download.tmp']);
  assert.equal(result.decision,'ALLOW');
  assert.deepEqual(result.checks,['PASS','PASS']);
});

test('unknown, absent, and empty evidence never allow an action', () => {
  for (const result of [assessRequest(['cache.tmp'],false), assessRequest(['unobserved.tmp']), assessRequest([])]) {
    assert.equal(result.result,'UNKNOWN');
    assert.equal(result.decision,'DENY');
  }
});

test('a protected target remains denied inside the batch limit', () => {
  assert.deepEqual(assessRequest(['orders.csv']).checks,['BLOCKED','PASS']);
  assert.equal(assessRequest(['orders.csv']).decision,'DENY');
});
