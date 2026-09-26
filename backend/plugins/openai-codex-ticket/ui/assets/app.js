(function () {
  "use strict";
  var bridge = window.sub2apiPluginBridge;
  var form = document.getElementById("config-form");
  var message = document.getElementById("message");
  var status = document.getElementById("status");
  var statusDetail = document.getElementById("status-detail");
  var busy = false;
  var defaults = { enabled: false, target_length: 292, ttl_seconds: 3600, refresh_before_seconds: 600, harvest_probe_interval_seconds: 180, harvest_cooldown_seconds: 180, max_probes_per_round: 6, harvest_attempt_timeout_seconds: 25, models: ["gpt-6-astra", "gpt-5.6-sol", "gpt-6-sol"], harvest_proxy_url: "", fail_closed: false, target_gateway: "unified-88", transport: "sse", ticket_url: "https://chatgpt.com/backend-api/codex/responses", harvest_url: "https://chatgpt.com/backend-api/codex/responses", cookie_validation: true, gateway_validation: true, team_plan_blocked: false };
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
  async function save() { var result = await bridge.request("config.save", { config: getConfig() }); setConfig(result.config || getConfig()); setMessage("配置已保存"); }
  async function test() { await save(); var result = await bridge.request("config.test"); var out = result.result || {}; setMessage(out.message || "配置有效", out.success === false); }
  function safeJSON(raw) { try { return raw ? JSON.parse(raw) : {}; } catch (_) { return {}; } }
  async function refreshStatus() {
    try {
      var result = await bridge.request("plugin.status"); var health = result.result || {}; var details = safeJSON(health.status_json);
      status.textContent = health.healthy ? (health.message || "运行中") : (health.message || "未运行");
      var tickets = Array.isArray(details.tickets) ? details.tickets.slice(-200).reverse().map(function (item) {
        // Keep the UI defensive even if an older or faulty runtime includes a
        // secret field in status_json. Only render non-sensitive summary keys.
        var safe = {};
        ["account_id", "model", "length", "ready", "remaining_seconds", "expires_at", "standby", "standby_expires_at", "transport", "gateway", "edge_ip", "cookie_count", "cookie_expires_at", "revoked"].forEach(function (key) { if (item && item[key] !== undefined) safe[key] = item[key]; });
        return safe;
      }) : [];
      var rawTickets = Array.isArray(details.tickets) ? details.tickets : [];
      var ready = rawTickets.filter(function (item) { return item && item.ready === true; }).length;
      var blocked = rawTickets.filter(function (item) { return item && item.blocked === true; }).length;
      statusDetail.textContent = health.healthy ? "账号 " + (details.accounts_total === undefined ? new Set(rawTickets.map(function (item) { return item && item.account_id; })).size : details.accounts_total) + " · 可用票 " + (details.ready_tickets === undefined ? ready : details.ready_tickets) + " · 阻止调度 " + (details.blocked_tickets === undefined ? blocked : details.blocked_tickets) + " · KV " + (details.host_kv ? "已连接" : "未连接") : "";
      document.getElementById("tickets-detail").textContent = tickets.length ? JSON.stringify(tickets, null, 2) : "暂无 ticket 摘要";
    } catch (error) { status.textContent = error.message; document.getElementById("tickets-detail").textContent = "无法刷新 ticket 摘要：" + error.message; }
  }
  async function run(action) { if (busy) return; busy = true; document.getElementById("save").disabled = true; document.getElementById("test").disabled = true; try { await action(); } catch (error) { setMessage(error.message, true); } finally { busy = false; document.getElementById("save").disabled = false; document.getElementById("test").disabled = false; } }
  form.addEventListener("submit", function (event) { event.preventDefault(); void run(save); });
  document.getElementById("test").addEventListener("click", function () { void run(test); });
  var statusTimer = setInterval(refreshStatus, 10000);
  window.addEventListener("pagehide", function () { clearInterval(statusTimer); bridge.dispose(); });
  bridge.resize(780);
  bridge.request("config.load").then(function (result) { setConfig(result.config || {}); return refreshStatus(); }).catch(function (error) { setMessage(error.message, true); });
}());
