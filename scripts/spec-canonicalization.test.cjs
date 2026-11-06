// RFC 8785 reference for the fixed GuardrailSpec schema: sorted ASCII keys,
// strings, arrays and the one positive safe-integer field. No external package.
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

function canonicalize(value) {
  if (Array.isArray(value)) return '[' + value.map(canonicalize).join(',') + ']';
  if (value !== null && typeof value === 'object') {
    return '{' + Object.keys(value).sort().map(key => JSON.stringify(key) + ':' + canonicalize(value[key])).join(',') + '}';
  }
  if (typeof value === 'number') assert.ok(Number.isSafeInteger(value) && value > 0);
  return JSON.stringify(value);
}

const vectors = JSON.parse(fs.readFileSync(path.join(__dirname, '../internal/domain/spec/testdata/canonical-vectors.json'), 'utf8'));
for (const vector of vectors) {
  test('ECMAScript canonical identity: ' + vector.name, () => {
    const document = JSON.parse(vector.canonical);
    const actual = canonicalize(document);
    assert.equal(actual, vector.canonical);
    assert.equal(crypto.createHash('sha256').update(actual, 'utf8').digest('hex'), vector.sha256);
    assert.equal(String(document.spec.validForSeconds), vector.validForSeconds ?? '3600');
  });
}

test('unsafe integer round-trip explains the rejected Spec boundary', () => {
  const rounded = JSON.parse('{"validForSeconds":9007199254740993}').validForSeconds;
  assert.equal(rounded, 9007199254740992);
  assert.equal(Number.isSafeInteger(rounded), false);
  assert.throws(() => canonicalize({validForSeconds: rounded}));
});
