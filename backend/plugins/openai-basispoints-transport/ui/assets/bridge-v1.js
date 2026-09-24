(function () {
  "use strict";
  var hash = new URLSearchParams((location.hash || "").slice(1));
  var token = hash.get("bridge_token") || "";
  var pending = new Map();
  var sequence = 0;

  function request(type, payload) {
    var requestId = "plugin-" + Date.now().toString(36) + "-" + (++sequence);
    return new Promise(function (resolve, reject) {
      var timer = setTimeout(function () {
        pending.delete(requestId);
        reject(new Error("宿主响应超时"));
      }, 30000);
      pending.set(requestId, { resolve: resolve, reject: reject, timer: timer, type: type });
      parent.postMessage(Object.assign({
        source: "sub2api-plugin-ui",
        bridge_token: token,
        type: type,
        request_id: requestId
      }, payload || {}), "*");
    });
  }

  function onMessage(event) {
    if (event.source !== parent || !event.data || event.data.source !== "sub2api-plugin-host" || event.data.bridge_token !== token) return;
    var item = pending.get(event.data.request_id);
    if (!item || event.data.type !== item.type + ".result") return;
    pending.delete(event.data.request_id);
    clearTimeout(item.timer);
    if (event.data.ok === false) item.reject(new Error(event.data.message || (event.data.result && event.data.result.message) || "宿主请求失败"));
    else item.resolve(event.data);
  }
  window.addEventListener("message", onMessage);

  window.sub2apiPluginBridge = {
    request: request,
    dispose: function () {
      window.removeEventListener("message", onMessage);
      pending.forEach(function (item) { clearTimeout(item.timer); item.reject(new Error("配置页面已关闭")); });
      pending.clear();
    },
    notify: function (level, message) { parent.postMessage({ source: "sub2api-plugin-ui", bridge_token: token, type: "ui.notify", request_id: "notify-" + Date.now(), level: level, message: message }, "*"); },
    resize: function (height) { parent.postMessage({ source: "sub2api-plugin-ui", bridge_token: token, type: "ui.resize", request_id: "resize-" + Date.now(), height: height }, "*"); }
  };
  parent.postMessage({ source: "sub2api-plugin-ui", bridge_token: token, type: "sub2api.plugin.ready", request_id: "ready-" + Date.now() }, "*");
}());
