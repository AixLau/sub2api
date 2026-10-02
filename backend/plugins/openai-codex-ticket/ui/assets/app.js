(function () {
  "use strict";
  var bridge = window.sub2apiPluginBridge;
  var form = document.getElementById("config-form");
  var message = document.getElementById("message");
  var status = document.getElementById("status");
  var statusDetail = document.getElementById("status-detail");
  var busy = false;
  var defaults = { enabled: false, role: "business", dry_run: false, log_decisions: true, block_degraded: false, template_length: 292, replace_length: 312, target_length: 780, ttl_seconds: 240, route_cookie_ttl_seconds: 3900, refresh_before_seconds: 60, harvest_probe_interval_seconds: 180, harvest_cooldown_seconds: 180, max_probes_per_round: 6, harvest_attempt_timeout_seconds: 25, models: ["gpt-6-astra", "gpt-5.6-sol"], harvest_proxy_url: "", harvest_proxy_pool: [], harvest_proxy_mode: "static", harvest_proxy_api_url: "", fail_closed: false, target_gateway: "unified-88", transport: "sse", ticket_url: "https://chatgpt.com/backend-api/codex/responses", harvest_url: "https://chatgpt.com/backend-api/codex/responses", cookie_validation: true, gateway_validation: true, team_plan_blocked: false, cloud_mint: { enabled: false, url: "", proxy_url: "", proxy_env: "", key_env: "CPA_RELAY_KEY", transport: "sse", gateway: "any", ticket_length: 780, ttl_seconds: 240, wait_ms: 2000, timeout_ms: 90000, fail_closed: true, mint_model: "gpt-6-astra" } };
  var ids = ["role", "template_length", "replace_length", "target_length", "ttl_seconds", "route_cookie_ttl_seconds", "refresh_before_seconds", "harvest_probe_interval_seconds", "harvest_cooldown_seconds", "max_probes_per_round", "harvest_attempt_timeout_seconds", "target_gateway", "transport", "ticket_url", "harvest_url", "harvest_proxy_url", "harvest_proxy_mode", "harvest_proxy_api_url"];
  var bools = ["enabled", "dry_run", "log_decisions", "fail_closed", "cookie_validation", "gateway_validation", "team_plan_blocked", "block_degraded"];
  var nums = ["template_length", "replace_length", "target_length", "ttl_seconds", "route_cookie_ttl_seconds", "refresh_before_seconds", "harvest_probe_interval_seconds", "harvest_cooldown_seconds", "max_probes_per_round", "harvest_attempt_timeout_seconds"];
  var cloudIds = ["url", "proxy_url", "proxy_env", "key_env", "transport", "gateway", "mint_model"];
  var cloudBools = ["enabled", "fail_closed"];
  var cloudNums = ["ticket_length", "ttl_seconds", "wait_ms", "timeout_ms"];
  function setMessage(text, error) { message.textContent = text || ""; message.style.color = error ? "#dc2626" : "#2563eb"; }
  function setConfig(input) {
    var config = Object.assign({}, defaults, input || {});
    config.cloud_mint = Object.assign({}, defaults.cloud_mint, input && input.cloud_mint || {});
    ids.forEach(function (id) { var node = document.getElementById(id); if (node) node.value = config[id] === undefined ? "" : config[id]; });
    bools.forEach(function (id) { document.getElementById(id).checked = config[id] === true; });
    document.getElementById("models").value = (config.models || []).join("\n");
    document.getElementById("harvest_proxy_pool").value = (config.harvest_proxy_pool || []).join("\n");
    cloudIds.forEach(function (id) { var node = document.getElementById("cloud_mint_" + id); if (node) node.value = config.cloud_mint[id] === undefined ? "" : config.cloud_mint[id]; });
    cloudBools.forEach(function (id) { document.getElementById("cloud_mint_" + id).checked = config.cloud_mint[id] === true; });
    cloudNums.forEach(function (id) { var node = document.getElementById("cloud_mint_" + id); if (node) node.value = config.cloud_mint[id] === undefined ? "" : config.cloud_mint[id]; });
  }
  function getConfig() {
    var config = {};
    ids.forEach(function (id) { var node = document.getElementById(id); config[id] = nums.indexOf(id) >= 0 ? Number(node.value) : node.value.trim(); });
    bools.forEach(function (id) { config[id] = document.getElementById(id).checked; });
    config.models = document.getElementById("models").value.split(/\r?\n/).map(function (m) { return m.trim(); }).filter(Boolean);
    config.harvest_proxy_pool = document.getElementById("harvest_proxy_pool").value.split(/\r?\n/).map(function (m) { return m.trim(); }).filter(Boolean);
    config.cloud_mint = {};
    cloudIds.forEach(function (id) { var node = document.getElementById("cloud_mint_" + id); config.cloud_mint[id] = node.value.trim(); });
    cloudBools.forEach(function (id) { config.cloud_mint[id] = document.getElementById("cloud_mint_" + id).checked; });
    cloudNums.forEach(function (id) { config.cloud_mint[id] = Number(document.getElementById("cloud_mint_" + id).value); });
    return config;
  }
  function safeJSON(raw) { try { return raw ? JSON.parse(raw) : {}; } catch (_) { return {}; } }
  function redact(value) { return String(value || "").replace(/(?:https?|socks5h?):\/\/[^\s/]*@/gi, "[代理凭据已隐藏]@").replace(/(?:x-codex-turn-state|authorization|access_token|refresh_token|api_key|password)\s*[:=]\s*[^\s,;]+/gi, "[敏感字段已隐藏]").slice(0, 300); }
  function formatRemaining(value) { var seconds = Number(value); if (!Number.isFinite(seconds) || seconds <= 0) return "已过期"; var minutes = Math.floor(seconds / 60); return minutes ? minutes + " 分 " + Math.floor(seconds % 60) + " 秒" : Math.floor(seconds) + " 秒"; }
  function accountLabel(id) { return "账号 #" + id; }
  function eventResult(event) { var map = { ready: "成功 · ticket 可用", harvest_failed: "采票失败", identity_unavailable: "账号身份不可用", invalid_state: "状态校验失败", ticket_persistence_failed: "保存失败", model_degraded: "响应模型降级", model_observed: "响应模型已观测", team_plan_skipped: "Team 计划已跳过" }; return map[event.result] || redact(event.result || "未知结果"); }
  function renderStatus(details, health) {
    var tickets = Array.isArray(details.tickets) ? details.tickets.slice().sort(function (a, b) { return Number(a.account_id) - Number(b.account_id) || String(a.model).localeCompare(String(b.model)); }) : [];
    var events = Array.isArray(details.events) ? details.events.slice().reverse() : [];
    var unique = new Set(tickets.map(function (item) { return item && item.account_id; })).size;
    var ready = tickets.filter(function (item) { return item && item.ready === true; }).length;
    var failed = events.filter(function (item) { return item && item.result && item.result !== "ready"; }).length;
    var counters = details.counters || {};
    statusDetail.textContent = health.healthy ? "账号 " + (details.accounts_total === undefined ? unique : details.accounts_total) + " · 可用票 " + (details.ready_tickets === undefined ? ready : details.ready_tickets) + " · 事件 " + events.length + " · 失败事件 " + failed + " · 决策 H" + (counters.harvest || 0) + "/R" + (counters.steer || 0) + "/P" + (counters.pass || 0) + "/S" + (counters.skip || 0) + " · KV " + (details.host_kv ? "已连接" : "未连接") : "";
    var ticketsNode = document.getElementById("tickets-detail"); ticketsNode.replaceChildren();
    if (!tickets.length) ticketsNode.textContent = "暂无有效 ticket 摘要";
    tickets.forEach(function (item) {
      var card = document.createElement("article"); card.className = "ticket-card" + (item.ready ? " ready" : "");
      var title = document.createElement("div"); title.className = "ticket-title"; title.textContent = accountLabel(item.account_id) + " · " + (item.model || "未知模型"); card.appendChild(title);
      var state = document.createElement("span"); state.className = "badge " + (item.ready ? "success" : "warning"); state.textContent = item.ready ? "可用" : (item.revoked ? "已撤销" : "不可用"); card.appendChild(state);
      var meta = document.createElement("div"); meta.className = "ticket-meta"; meta.textContent = [item.length ? "STATE " + item.length : "", item.transport || "", item.gateway || "", item.ready ? "剩余 " + formatRemaining(item.remaining_seconds) : "", item.cookie_count ? "Cookie " + item.cookie_count : ""].filter(Boolean).join(" · "); card.appendChild(meta);
      ticketsNode.appendChild(card);
    });
    var routesNode = document.getElementById("routes-detail"); routesNode.replaceChildren();
    var routes = Array.isArray(details.route_pairs) ? details.route_pairs : [];
    if (!routes.length) routesNode.textContent = "暂无路由 Cookie";
    routes.forEach(function (item) {
      var card = document.createElement("article"); card.className = "ticket-card" + (Number(item.remaining_seconds) > 0 ? " ready" : "");
      var title = document.createElement("div"); title.className = "ticket-title"; title.textContent = (item.gateway || "未知 Gateway") + " · " + (item.fingerprint || "无指纹"); card.appendChild(title);
      var meta = document.createElement("div"); meta.className = "ticket-meta"; meta.textContent = [Number(item.remaining_seconds) > 0 ? "剩余 " + formatRemaining(item.remaining_seconds) : "已过期", item.via ? "出口 " + item.via : "", item.bad_at ? "最近降级" : "", item.good_at ? "已验证" : ""].filter(Boolean).join(" · "); card.appendChild(meta);
      routesNode.appendChild(card);
    });
    var observationsNode = document.getElementById("observations-detail"); observationsNode.replaceChildren();
    var observations = Array.isArray(details.observations) ? details.observations.slice().sort(function (a, b) { return Number(a.account_id) - Number(b.account_id) || String(a.model).localeCompare(String(b.model)); }) : [];
    if (!observations.length) observationsNode.textContent = "暂无响应观测";
    observations.forEach(function (item) {
      var card = document.createElement("article"); card.className = "ticket-card";
      var title = document.createElement("div"); title.className = "ticket-title"; title.textContent = accountLabel(item.account_id) + " · " + (item.model || "未知模型"); card.appendChild(title);
      var meta = document.createElement("div"); meta.className = "ticket-meta"; meta.textContent = [item.last_served ? "served " + item.last_served : "", item.last_ticket_kind ? "最近 " + item.last_ticket_kind : "", item.matches ? "匹配 " + item.matches : "", item.degradations ? "降级 " + item.degradations : "", item.injected_silent ? "注入静默 " + item.injected_silent : ""].filter(Boolean).join(" · "); card.appendChild(meta);
      observationsNode.appendChild(card);
    });
    var recentNode = document.getElementById("observation-recent-detail"); recentNode.replaceChildren();
    var recent = Array.isArray(details.observation_recent) ? details.observation_recent.slice().reverse() : [];
    if (!recent.length) recentNode.textContent = "暂无最近响应事件";
    recent.slice(0, 100).forEach(function (event) {
      var row = document.createElement("div"); row.className = "event-row";
      var when = document.createElement("span"); when.className = "event-time"; when.textContent = event.at ? new Date(event.at).toLocaleTimeString("zh-CN") : "—";
      var who = document.createElement("span"); who.className = "event-who"; who.textContent = accountLabel(event.account_id) + " · " + (event.model || "未知模型");
      var detail = document.createElement("span"); detail.className = "event-detail"; detail.textContent = [(event.kind || "未知"), event.length > 0 ? "STATE " + event.length : "静默", event.injected ? "注入" : "自然", event.served ? "served " + event.served : ""].filter(Boolean).join(" · ");
      [when, who, detail].forEach(function (node) { row.appendChild(node); }); recentNode.appendChild(row);
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
    catch (error) { status.textContent = redact(error.message); statusDetail.textContent = "无法刷新插件状态"; document.getElementById("tickets-detail").textContent = "无法刷新 ticket 摘要"; document.getElementById("routes-detail").textContent = "无法刷新路由 Cookie"; document.getElementById("observations-detail").textContent = "无法刷新响应观测"; document.getElementById("observation-recent-detail").textContent = "无法刷新最近响应事件"; document.getElementById("events-detail").textContent = "无法刷新采票流水"; }
  }
  async function run(action) { if (busy) return; busy = true; document.getElementById("save").disabled = true; document.getElementById("test").disabled = true; try { await action(); } catch (error) { setMessage(redact(error.message), true); } finally { busy = false; document.getElementById("save").disabled = false; document.getElementById("test").disabled = false; } }
  form.addEventListener("submit", function (event) { event.preventDefault(); void run(save); });
  document.getElementById("test").addEventListener("click", function () { void run(test); });
  var statusTimer = setInterval(refreshStatus, 5000);
  window.addEventListener("pagehide", function () { clearInterval(statusTimer); bridge.dispose(); });
  bridge.resize(900);
  bridge.request("config.load").then(function (result) { setConfig(result.config || {}); return refreshStatus(); }).catch(function (error) { setMessage(redact(error.message), true); });
}());
