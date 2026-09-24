const { test } = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const vm = require('node:vm');

function runtime() {
  const messages = [], timers = new Map(), listeners = new Map();
  let sequence = 0;
  const parent = { postMessage(message) { messages.push(message); } };
  const window = {
    addEventListener(type, fn) { listeners.set(type, fn); },
    removeEventListener(type, fn) { if (listeners.get(type) === fn) listeners.delete(type); }
  };
  const context = vm.createContext({
    parent, window, location: { hash: '#bridge_token=test-token' }, URLSearchParams,
    setTimeout(fn) { timers.set(++sequence, fn); return sequence; },
    clearTimeout(id) { timers.delete(id); }
  });
  vm.runInContext(readFileSync(join(__dirname, '../ui/assets/bridge-v1.js'), 'utf8'), context);
  function respond(request, overrides = {}, source = parent) {
    listeners.get('message')?.({ source, data: {
      source: 'sub2api-plugin-host', bridge_token: 'test-token', request_id: request.request_id,
      type: request.type + '.result', ok: true, ...overrides
    } });
  }
  return { bridge: window.sub2apiPluginBridge, messages, timers, listeners, respond };
}

test('only the matching host response resolves a config request', async () => {
  const r = runtime();
  const promise = r.bridge.request('config.load');
  const request = r.messages.at(-1);
  r.respond(request, {}, {});
  r.respond(request, { bridge_token: 'wrong' });
  r.respond(request, { type: 'config.save.result' });
  assert.equal(r.timers.size, 1);
  r.respond(request, { config: { request_timeout_seconds: 60 } });
  assert.equal((await promise).config.request_timeout_seconds, 60);
  assert.equal(r.timers.size, 0);
});

test('test failure preserves the host diagnostic', async () => {
  const r = runtime();
  const promise = r.bridge.request('config.test');
  r.respond(r.messages.at(-1), { ok: false, result: { success: false, message: 'KV unavailable' } });
  await assert.rejects(promise, /KV unavailable/);
});

test('timeout and disposal release pending requests and listeners', async () => {
  const r = runtime();
  const promise = r.bridge.request('config.load');
  const timeout = r.timers.values().next().value;
  timeout();
  await assert.rejects(promise, /超时/);
  const next = r.bridge.request('plugin.status');
  r.bridge.dispose();
  await assert.rejects(next, /关闭/);
  assert.equal(r.listeners.has('message'), false);
});
