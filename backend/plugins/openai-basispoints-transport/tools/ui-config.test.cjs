const assert = require('node:assert/strict');
const { test } = require('node:test');
const { readFileSync } = require('node:fs');
const path = require('node:path');
const { createRequire } = require('node:module');
const { JSDOM } = createRequire(path.resolve(__dirname, '../../../../frontend/package.json'))('jsdom');
const newline = String.fromCharCode(10);
const uiDir = path.join(__dirname, '../ui');

async function mount(t, config = {}) {
  const dom = new JSDOM(readFileSync(path.join(uiDir, 'index.html'), 'utf8'), { runScripts: 'outside-only' });
  const calls = [];
  dom.window.sub2apiPluginBridge = {
    resize() {}, dispose() {},
    async request(method, params) {
      calls.push({ method, params });
      if (method === 'config.load') return { config };
      if (method === 'config.save') return { config: params.config };
      return { result: { healthy: true, status_json: '{}' } };
    }
  };
  dom.window.eval(readFileSync(path.join(uiDir, 'assets/app.js'), 'utf8'));
  t.after(() => dom.window.close());
  await new Promise(setImmediate);
  return { window: dom.window, field: id => dom.window.document.getElementById(id), calls };
}

test('model selection defaults to all and round trips exact selected names', async t => {
  const { window, field, calls } = await mount(t);
  assert.equal(field('bps_model_mode').value, 'all');
  assert.equal(field('bps_models').disabled, true);
  field('bps_model_mode').value = 'selected';
  field('bps_model_mode').dispatchEvent(new window.Event('change'));
  assert.equal(field('bps_models').disabled, false);
  field('bps_models').value = [' model-a-excel ', '', 'Model-B', ''].join(newline);
  field('config-form').dispatchEvent(new window.Event('submit', { cancelable: true }));
  await new Promise(setImmediate);
  const saved = calls.find(call => call.method === 'config.save').params.config;
  assert.equal(saved.bps_model_mode, 'selected');
  assert.equal(JSON.stringify(saved.bps_models), JSON.stringify(['model-a-excel', 'Model-B']));
  assert.equal(saved.native_fallback, true);
  assert.equal(field('bps_models').value, ['model-a-excel', 'Model-B'].join(newline));
  assert.equal(field('message').textContent, '配置已保存');
});

test('selected empty list is retained and all mode preserves the editable list', async t => {
  const { window, field, calls } = await mount(t, { bps_model_mode: 'selected', bps_models: [] });
  assert.equal(field('bps_models').disabled, false);
  field('config-form').dispatchEvent(new window.Event('submit', { cancelable: true }));
  await new Promise(setImmediate);
  const empty = calls.find(call => call.method === 'config.save').params.config;
  assert.equal(empty.bps_model_mode, 'selected');
  assert.equal(empty.bps_models.length, 0);
  field('bps_models').value = 'model-a';
  field('bps_model_mode').value = 'all';
  field('bps_model_mode').dispatchEvent(new window.Event('change'));
  assert.equal(field('bps_models').disabled, true);
  field('config-form').dispatchEvent(new window.Event('submit', { cancelable: true }));
  await new Promise(setImmediate);
  const saved = calls.filter(call => call.method === 'config.save').at(-1).params.config;
  assert.equal(saved.bps_model_mode, 'all');
  assert.equal(JSON.stringify(saved.bps_models), JSON.stringify(['model-a']));
});
