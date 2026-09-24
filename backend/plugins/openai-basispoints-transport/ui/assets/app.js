(function () {
  "use strict";
  var bridge = window.sub2apiPluginBridge;
  var form = document.getElementById("config-form");
  var message = document.getElementById("message");
  var status = document.getElementById("status");
  var statusDetail = document.getElementById("status-detail");
  var busy = false;
  var defaults = { upstream_base_url: "https://bps.openai.com/basispoints/api", proxy_mode: "account", request_timeout_seconds: 120, response_header_timeout_seconds: 30, idle_connection_timeout_seconds: 90, max_idle_connections: 100, max_idle_connections_per_host: 20, tls_min_version: "1.2", enable_http2: true, extra_headers: {} };
  var ids = ["upstream_base_url", "proxy_mode", "request_timeout_seconds", "response_header_timeout_seconds", "idle_connection_timeout_seconds", "max_idle_connections", "max_idle_connections_per_host", "tls_min_version"];

  function setMessage(text, error) { message.textContent = text || ""; message.style.color = error ? "#dc2626" : "#2563eb"; }
  function setConfig(config) {
    config = Object.assign({}, defaults, config);
    ids.forEach(function (id) { var node = document.getElementById(id); if (node && config[id] !== undefined) node.value = config[id]; });
    document.getElementById("enable_http2").checked = config.enable_http2 !== false;
    document.getElementById("extra_headers").value = JSON.stringify(config.extra_headers || {}, null, 2);
  }
  function getConfig() {
    var extra;
    try { extra = JSON.parse(document.getElementById("extra_headers").value || "{}"); } catch (_) { throw new Error("额外请求头必须是有效 JSON"); }
    if (!extra || Array.isArray(extra) || typeof extra !== "object") throw new Error("额外请求头必须是 JSON 对象");
    var config = {};
    ids.forEach(function (id) { var node = document.getElementById(id); config[id] = id.indexOf("_seconds") >= 0 || id.indexOf("connections") >= 0 ? Number(node.value) : node.value; });
    config.auth_mode = "chatgpt";
    config.enable_http2 = document.getElementById("enable_http2").checked;
    config.extra_headers = extra;
    return config;
  }
  async function save() {
    var result = await bridge.request("config.save", { config: getConfig() });
    setConfig(result.config || getConfig());
    setMessage("配置已保存");
  }
  async function test() {
    await save();
    var result = await bridge.request("config.test");
    setMessage(result.result && result.result.message ? result.result.message : "配置有效", result.result && !result.result.success);
  }
  async function load() {
    var result = await bridge.request("config.load");
    setConfig(result.config || {});
    await refreshStatus();
  }
  async function refreshStatus() {
    try {
      var result = await bridge.request("plugin.status");
      var health = result.result || {};
      status.textContent = health.healthy ? (health.message || "运行中") : (health.message || "未运行");
      var details = health.status_json ? JSON.parse(health.status_json) : {};
      statusDetail.textContent = health.healthy ? "请求 " + (details.requests_total || 0) + " · 成功 " + (details.requests_succeeded || 0) + " · 失败 " + (details.requests_failed || 0) + " · 最近 HTTP " + (details.last_status_code || "—") + " · 工具回放存储：" + (details.host_kv ? "已连接" : "未连接") + (details.last_bridge_error ? " · 最近桥接错误：" + details.last_bridge_error : "") + (details.omitted_hosted_tools ? " · 未支持的托管工具：" + details.omitted_hosted_tools : "") : "";
    } catch (error) { status.textContent = error.message; }
  }
  async function run(action) {
    if (busy) return;
    busy = true;
    document.getElementById("save").disabled = true;
    document.getElementById("test").disabled = true;
    try { await action(); } catch (error) { setMessage(error.message, true); }
    finally { busy = false; document.getElementById("save").disabled = false; document.getElementById("test").disabled = false; }
  }
  form.addEventListener("submit", function (event) { event.preventDefault(); void run(save); });
  document.getElementById("test").addEventListener("click", function () { void run(test); });
  var statusTimer = setInterval(refreshStatus, 10000);
  window.addEventListener("pagehide", function () { clearInterval(statusTimer); bridge.dispose(); });
  bridge.resize(720);
  load().catch(function (error) { setMessage(error.message, true); });
}());
