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
      }, 15000);
      pending.set(requestId, { resolve: resolve, reject: reject, timer: timer });
      parent.postMessage(Object.assign({
        source: "sub2api-plugin-ui",
        bridge_token: token,
        type: type,
        request_id: requestId
      }, payload || {}), "*");
    });
  }

  window.addEventListener("message", function (event) {
    if (event.source !== parent || !event.data || event.data.source !== "sub2api-plugin-host" || event.data.bridge_token !== token) return;
    var item = pending.get(event.data.request_id);
    if (!item) return;
    pending.delete(event.data.request_id);
    clearTimeout(item.timer);
    if (event.data.ok === false) item.reject(new Error(event.data.message || "宿主请求失败"));
    else item.resolve(event.data);
  });

  window.sub2apiPluginBridge = {
    request: request,
    notify: function (level, message) { parent.postMessage({ source: "sub2api-plugin-ui", bridge_token: token, type: "ui.notify", request_id: "notify-" + Date.now(), level: level, message: message }, "*"); },
    resize: function (height) { parent.postMessage({ source: "sub2api-plugin-ui", bridge_token: token, type: "ui.resize", request_id: "resize-" + Date.now(), height: height }, "*"); }
  };
  parent.postMessage({ source: "sub2api-plugin-ui", bridge_token: token, type: "sub2api.plugin.ready", request_id: "ready-" + Date.now() }, "*");
}());
