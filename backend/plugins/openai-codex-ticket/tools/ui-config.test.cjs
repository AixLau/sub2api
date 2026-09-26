const assert = require('node:assert/strict');
const { test } = require('node:test');
const { readFileSync } = require('node:fs');
const path = require('node:path');
const { createRequire } = require('node:module');
const { JSDOM } = createRequire(path.resolve(__dirname, '../../../../frontend/package.json'))('jsdom');
const uiDir = path.join(__dirname, '../ui');

async function mount(t, config = {}, statusDetails = {}) {
  const dom = new JSDOM(readFileSync(path.join(uiDir, 'index.html'), 'utf8'), { runScripts: 'outside-only' });
  const calls = [];
  dom.window.sub2apiPluginBridge = {
    resize() {}, dispose() {},
    async request(method, params) {
      calls.push({ method, params });
      if (method === 'config.load') return { config };
      if (method === 'config.save') return { config: params.config };
      if (method === 'config.test') return { result: { success: true, message: '配置有效' } };
      return { result: { healthy: true, message: 'worker running', status_json: JSON.stringify(statusDetails) } };
    }
  };
  dom.window.eval(readFileSync(path.join(uiDir, 'assets/app.js'), 'utf8'));
  t.after(() => dom.window.close());
  await new Promise(setImmediate);
  return { window: dom.window, field: id => dom.window.document.getElementById(id), calls };
}

test('loads defaults and round trips ticket configuration', async t => {
  const { window, field, calls } = await mount(t);
  assert.equal(field('enabled').checked, false);
  assert.equal(field('target_length').value, '292');
  field('enabled').checked = true;
  field('target_length').value = '780';
  field('models').value = ' model-a\n\nmodel-b ';
  field('fail_closed').checked = true;
  field('config-form').dispatchEvent(new window.Event('submit', { cancelable: true }));
  await new Promise(setImmediate);
  const saved = calls.find(call => call.method === 'config.save').params.config;
  assert.equal(saved.enabled, true);
  assert.equal(saved.target_length, 780);
  assert.equal(JSON.stringify(saved.models), JSON.stringify(['model-a', 'model-b']));
  assert.equal(saved.fail_closed, true);
  assert.equal(field('message').textContent, '配置已保存');
});

test('renders bounded status summaries as text and never exposes state', async t => {
  const state = '<img src=x onerror=alert(1)>'; // should remain inert if a faulty host sends it
  const tickets = Array.from({ length: 210 }, (_, n) => ({ account_id: n, model: 'model-' + n, ready: true, state }));
  const { field } = await mount(t, {}, { accounts_total: 210, ready_tickets: 210, blocked_tickets: 0, host_kv: true, tickets });
  const rendered = field('tickets-detail').textContent;
  assert.doesNotMatch(rendered, /<img/);
  assert.equal(field('tickets-detail').querySelector('img'), null);
  assert.match(field('status-detail').textContent, /账号 210/);
  assert.match(field('status-detail').textContent, /可用票 210/);
});

test('shows explicit empty state when worker has no tickets', async t => {
  const { field } = await mount(t, {}, { accounts_total: 0, ready_tickets: 0, blocked_tickets: 0, tickets: [] });
  assert.equal(field('tickets-detail').textContent, '暂无 ticket 摘要');
});
