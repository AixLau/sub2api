(function () {
  "use strict";
  var bridge = window.sub2apiPluginBridge;
  var form = document.getElementById("config-form");
  var message = document.getElementById("message");
  var status = document.getElementById("status");
  var statusDetail = document.getElementById("status-detail");
  var busy = false;
  var defaults = { upstream_url: "https://bps.openai.com/basispoints/api/responses", upstream_transport: "ws", ws_idle_timeout_seconds: 120, tools_version: "", arc_text_session: false, proxy_mode: "account", native_passthrough: true, extra_headers: {} };
  var ids = ["upstream_url", "upstream_transport", "ws_idle_timeout_seconds", "tools_version", "proxy_mode"];

  function setMessage(text, error) { message.textContent = text || ""; message.style.color = error ? "#dc2626" : "#2563eb"; }
  function setConfig(config) {
    config = Object.assign({}, defaults, config);
    ids.forEach(function (id) { var node = document.getElementById(id); if (node && config[id] !== undefined) node.value = config[id]; });
    document.getElementById("native_passthrough").checked = config.native_passthrough !== false;
    document.getElementById("arc_text_session").checked = config.arc_text_session === true;
    document.getElementById("extra_headers").value = JSON.stringify(config.extra_headers || {}, null, 2);
  }
  function getConfig() {
    var extra;
    try { extra = JSON.parse(document.getElementById("extra_headers").value || "{}"); } catch (_) { throw new Error("额外请求头必须是有效 JSON"); }
    if (!extra || Array.isArray(extra) || typeof extra !== "object") throw new Error("额外请求头必须是 JSON 对象");
    var config = {};
    ids.forEach(function (id) { var node = document.getElementById(id); config[id] = id.indexOf("_seconds") >= 0 ? Number(node.value) : node.value; });
    config.native_passthrough = document.getElementById("native_passthrough").checked;
    config.arc_text_session = document.getElementById("arc_text_session").checked;
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
      var d = health.status_json ? JSON.parse(health.status_json) : {};
      statusDetail.textContent = health.healthy
        ? "传输 " + (d.upstream_transport || "—") + " · 请求 " + (d.requests_total || 0) + "（桥接 " + (d.requests_bridged || 0) + " / 原生透传 " + (d.requests_native || 0) + "）· 成功 " + (d.requests_succeeded || 0) + " · 失败 " + (d.requests_failed || 0) + " · 最近 HTTP " + (d.last_status_code || "—") + " · 最近耗时 " + (d.last_latency_ms || 0) + " ms" + (d.last_error ? " · 最近错误 " + d.last_error : "")
        : "";
    } catch (error) {
      status.textContent = error.message;
    }
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
  bridge.resize(640);
  load().catch(function (error) { setMessage(error.message, true); });
}());
