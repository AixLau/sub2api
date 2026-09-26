(function () {
  "use strict";
  var bridge = window.sub2apiPluginBridge;
  var form = document.getElementById("config-form");
  var message = document.getElementById("message");
  var status = document.getElementById("status");
  var statusDetail = document.getElementById("status-detail");
  var busy = false;
  var defaults = { enabled: false, target_length: 780, ttl_seconds: 240, refresh_before_seconds: 60, harvest_probe_interval_seconds: 180, harvest_cooldown_seconds: 180, max_probes_per_round: 6, harvest_attempt_timeout_seconds: 25, models: ["gpt-6-astra", "gpt-5.6-sol"], harvest_proxy_url: "", fail_closed: false, target_gateway: "unified-88", transport: "sse", ticket_url: "https://chatgpt.com/backend-api/codex/responses", harvest_url: "https://chatgpt.com/backend-api/codex/responses", cookie_validation: true, gateway_validation: true, team_plan_blocked: false };
  var ids = ["target_length", "ttl_seconds", "refresh_before_seconds", "harvest_probe_interval_seconds", "harvest_cooldown_seconds", "max_probes_per_round", "harvest_attempt_timeout_seconds", "target_gateway", "transport", "ticket_url", "harvest_url", "harvest_proxy_url"];
  var bools = ["enabled", "fail_closed", "cookie_validation", "gateway_validation", "team_plan_blocked"];
  var nums = ["target_length", "ttl_seconds", "refresh_before_seconds", "harvest_probe_interval_seconds", "harvest_cooldown_seconds", "max_probes_per_round", "harvest_attempt_timeout_seconds"];
  function setMessage(text, error) { message.textContent = text || ""; message.style.color = error ? "#dc2626" : "#2563eb"; }
  function setConfig(input) {
    var config = Object.assign({}, defaults, input || {});
    ids.forEach(function (id) { var node = document.getElementById(id); if (node) node.value = config[id] === undefined ? "" : config[id]; });
    bools.forEach(function (id) { document.getElementById(id).checked = config[id] === true; });
    document.getElementById("models").value = (config.models || []).join("\n");
  }
  function getConfig() {
    var config = {};
    ids.forEach(function (id) { var node = document.getElementById(id); config[id] = nums.indexOf(id) >= 0 ? Number(node.value) : node.value.trim(); });
    bools.forEach(function (id) { config[id] = document.getElementById(id).checked; });
    config.models = document.getElementById("models").value.split(/\r?\n/).map(function (m) { return m.trim(); }).filter(Boolean);
    return config;
  }
  function safeJSON(raw) { try { return raw ? JSON.parse(raw) : {}; } catch (_) { return {}; } }
  function redact(value) { return String(value || "").replace(/(?:https?|socks5h?):\/\/[^\s/]*@/gi, "[代理凭据已隐藏]@").replace(/(?:x-codex-turn-state|authorization|access_token|refresh_token|api_key|password)\s*[:=]\s*[^\s,;]+/gi, "[敏感字段已隐藏]").slice(0, 300); }
  function formatRemaining(value) { var seconds = Number(value); if (!Number.isFinite(seconds) || seconds <= 0) return "已过期"; var minutes = Math.floor(seconds / 60); return minutes ? minutes + " 分 " + Math.floor(seconds % 60) + " 秒" : Math.floor(seconds) + " 秒"; }
  function accountLabel(id) { return "账号 #" + id; }
  function eventResult(event) { var map = { ready: "成功 · ticket 可用", harvest_failed: "采票失败", identity_unavailable: "账号身份不可用", invalid_state: "状态校验失败", ticket_persistence_failed: "保存失败" }; return map[event.result] || redact(event.result || "未知结果"); }
  function renderStatus(details, health) {
    var tickets = Array.isArray(details.tickets) ? details.tickets.slice().sort(function (a, b) { return Number(a.account_id) - Number(b.account_id) || String(a.model).localeCompare(String(b.model)); }) : [];
    var events = Array.isArray(details.events) ? details.events.slice().reverse() : [];
    var unique = new Set(tickets.map(function (item) { return item && item.account_id; })).size;
    var ready = tickets.filter(function (item) { return item && item.ready === true; }).length;
    var failed = events.filter(function (item) { return item && item.result && item.result !== "ready"; }).length;
    statusDetail.textContent = health.healthy ? "账号 " + (details.accounts_total === undefined ? unique : details.accounts_total) + " · 可用票 " + (details.ready_tickets === undefined ? ready : details.ready_tickets) + " · 事件 " + events.length + " · 失败事件 " + failed + " · KV " + (details.host_kv ? "已连接" : "未连接") : "";
    var ticketsNode = document.getElementById("tickets-detail"); ticketsNode.replaceChildren();
    if (!tickets.length) ticketsNode.textContent = "暂无有效 ticket 摘要";
    tickets.forEach(function (item) {
      var card = document.createElement("article"); card.className = "ticket-card" + (item.ready ? " ready" : "");
      var title = document.createElement("div"); title.className = "ticket-title"; title.textContent = accountLabel(item.account_id) + " · " + (item.model || "未知模型"); card.appendChild(title);
      var state = document.createElement("span"); state.className = "badge " + (item.ready ? "success" : "warning"); state.textContent = item.ready ? "可用" : (item.revoked ? "已撤销" : "不可用"); card.appendChild(state);
      var meta = document.createElement("div"); meta.className = "ticket-meta"; meta.textContent = [item.length ? "STATE " + item.length : "", item.transport || "", item.gateway || "", item.ready ? "剩余 " + formatRemaining(item.remaining_seconds) : "", item.cookie_count ? "Cookie " + item.cookie_count : ""].filter(Boolean).join(" · "); card.appendChild(meta);
      ticketsNode.appendChild(card);
    });
    var eventsNode = document.getElementById("events-detail"); eventsNode.replaceChildren();
    if (!events.length) eventsNode.textContent = "暂无采票流水";
    events.slice(0, 200).forEach(function (event) {
      var row = document.createElement("div"); row.className = "event-row";
      var when = document.createElement("span"); when.className = "event-time"; when.textContent = event.time ? new Date(event.time).toLocaleTimeString("zh-CN") : "—";
      var who = document.createElement("span"); who.className = "event-who"; who.textContent = accountLabel(event.account_id) + " · " + (event.model || "未知模型");
      var phase = document.createElement("span"); phase.className = "event-phase"; phase.textContent = (event.phase || "采票") + (event.attempt ? " · 第 " + event.attempt + " 次" : "");
      var result = document.createElement("span"); result.className = "event-result " + (event.result === "ready" ? "ok" : "error"); result.textContent = eventResult(event);
      var detail = document.createElement("span"); detail.className = "event-detail"; detail.textContent = [event.state_bytes ? "STATE " + event.state_bytes : "", event.cookie_count ? "Cookie " + event.cookie_count : "", event.duration_ms >= 0 ? event.duration_ms + " ms" : "", event.error ? redact(event.error) : ""].filter(Boolean).join(" · ");
      [when, who, phase, result, detail].forEach(function (node) { row.appendChild(node); }); eventsNode.appendChild(row);
    });
  }
  async function save() { var result = await bridge.request("config.save", { config: getConfig() }); setConfig(result.config || getConfig()); setMessage("配置已保存"); }
  async function test() { await save(); var result = await bridge.request("config.test"); var out = result.result || {}; setMessage(out.message || "配置有效", out.success === false); }
  async function refreshStatus() {
    try { var result = await bridge.request("plugin.status"); var health = result.result || {}; var details = safeJSON(health.status_json); status.textContent = health.healthy ? (health.message || "运行中") : (health.message || "未运行"); status.className = "badge " + (health.healthy ? "success" : "warning"); renderStatus(details, health); }
    catch (error) { status.textContent = redact(error.message); statusDetail.textContent = "无法刷新插件状态"; document.getElementById("tickets-detail").textContent = "无法刷新 ticket 摘要"; document.getElementById("events-detail").textContent = "无法刷新采票流水"; }
  }
  async function run(action) { if (busy) return; busy = true; document.getElementById("save").disabled = true; document.getElementById("test").disabled = true; try { await action(); } catch (error) { setMessage(redact(error.message), true); } finally { busy = false; document.getElementById("save").disabled = false; document.getElementById("test").disabled = false; } }
  form.addEventListener("submit", function (event) { event.preventDefault(); void run(save); });
  document.getElementById("test").addEventListener("click", function () { void run(test); });
  var statusTimer = setInterval(refreshStatus, 5000);
  window.addEventListener("pagehide", function () { clearInterval(statusTimer); bridge.dispose(); });
  bridge.resize(900);
  bridge.request("config.load").then(function (result) { setConfig(result.config || {}); return refreshStatus(); }).catch(function (error) { setMessage(redact(error.message), true); });
}());
