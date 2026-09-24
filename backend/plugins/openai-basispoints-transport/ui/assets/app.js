(function () {
  "use strict";
  var bridge = window.sub2apiPluginBridge;
  var form = document.getElementById("config-form");
  var message = document.getElementById("message");
  var status = document.getElementById("status");
  var ids = ["upstream_base_url", "proxy_mode", "request_timeout_seconds", "response_header_timeout_seconds", "idle_connection_timeout_seconds", "max_idle_connections", "max_idle_connections_per_host", "tls_min_version"];

  function setMessage(text, error) { message.textContent = text || ""; message.style.color = error ? "#dc2626" : "#2563eb"; }
  function setConfig(config) {
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
    var result = await bridge.request("config.test");
    setMessage(result.result && result.result.message ? result.result.message : "配置有效");
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
    } catch (error) { status.textContent = error.message; }
  }
  form.addEventListener("submit", function (event) { event.preventDefault(); setMessage("保存中…"); save().catch(function (error) { setMessage(error.message, true); }); });
  document.getElementById("test").addEventListener("click", function () { setMessage("校验中…"); test().catch(function (error) { setMessage(error.message, true); }); });
  bridge.resize(720);
  load().catch(function (error) { setMessage(error.message, true); });
}());
