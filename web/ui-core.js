// ui-core.js — pure helpers shared by the admin UI and the node:test suite
// (波 3 JS 测试基建). No DOM here: anything in this file must run in a bare
// Node process so `node --test ui-core.test.mjs` can pin it.
(function (root, factory) {
  const core = factory();
  if (typeof module !== "undefined" && module.exports) module.exports = core;
  else root.XiaoyaoCore = core;
})(typeof self !== "undefined" ? self : this, function () {
  "use strict";

  function errorText(data, fallback) {
    if (!data) return fallback || "";
    return data.message || data.error || data.output || fallback || "";
  }

  // buildPairLink assembles the auto-filling phone link; defaultHost is the
  // browser's hostname (passed in at the call site to keep this pure).
  function buildPairLink(data, defaultHost) {
    const scheme = data.scheme || "https";
    const port = data.port || "8788";
    const base = data.link || (scheme + "://" + (defaultHost || "") + ":" + port + "/phone");
    return base + "#pair=" + data.code;
  }

  function fmtDuration(ms) {
    const total = Math.round((ms || 0) / 1000);
    if (total < 60) return total + " 秒";
    const minutes = Math.floor(total / 60);
    const rest = total % 60;
    if (minutes < 60) return rest ? minutes + " 分 " + rest + " 秒" : minutes + " 分";
    return Math.floor(minutes / 60) + " 小时 " + (minutes % 60) + " 分";
  }

  // needsRestart reads the server's restart_required contract. Some saves
  // ride a raw map payload rather than the typed reply, so both shapes are
  // accepted; anything else (undefined/false/absent) means live.
  function needsRestart(data) {
    return !!(data && (data.restart_required === true || data.restart_required === "true"));
  }

  // friendlyAsrError maps the engine's Go-level failures onto plain speech
  // for the hero line; unknown text passes through untouched so a novel
  // error is never swallowed.
  function friendlyAsrError(text) {
    const raw = (text || "").trim();
    if (!raw) return "";
    const lower = raw.toLowerCase();
    if (lower.includes("no such file") && (lower.includes("api key") || lower.includes("api_key") || lower.includes("key file"))) {
      return "识别密钥文件缺失：把 API Key 写进「语音识别」页配置的密钥文件路径";
    }
    if (lower.includes("no such file") && (lower.includes("whisper") || lower.includes("binary") || lower.includes("model"))) {
      return "本地识别程序或模型不存在：检查 Binary 与 Model Path";
    }
    if (lower.includes("no asr provider could be built")) {
      return "没有可用的识别引擎：到「语音识别」页配置云端接口或本地 whisper.cpp";
    }
    if (lower.includes("401") || lower.includes("unauthorized") || lower.includes("invalid api key")) {
      return "识别服务拒绝访问（401）：检查 API Key 是否有效";
    }
    if (lower.includes("connection refused") || lower.includes("dial tcp") || lower.includes("timeout") || lower.includes("deadline")) {
      return "连不上识别服务：检查 Base URL 与网络";
    }
    return raw;
  }

  // sealDiagPack encrypts the diagnostics pack (R10). Format OVBDIA1 —
  // PBKDF2-HMAC-SHA256 (600k) + AES-256-GCM with the 40-byte header as
  // additional data, mirrored by internal/diagbox. The AAD binding is load-
  // bearing: Go authenticates the header, so a version without it fails to
  // open in the CLI (caught by the WebCrypto-to-CLI cross-check).
  async function sealDiagPack(bytes, passphrase) {
    const enc = new TextEncoder();
    const salt = crypto.getRandomValues(new Uint8Array(16));
    const nonce = crypto.getRandomValues(new Uint8Array(12));
    const head = new Uint8Array(40);
    head.set(enc.encode("OVBDIA1\n"), 0);
    head.set(salt, 8);
    head.set(nonce, 24);
    new DataView(head.buffer).setUint32(36, 600000);
    const baseKey = await crypto.subtle.importKey("raw", enc.encode(passphrase), "PBKDF2", false, ["deriveKey"]);
    const key = await crypto.subtle.deriveKey(
      { name: "PBKDF2", hash: "SHA-256", salt, iterations: 600000 },
      baseKey, { name: "AES-GCM", length: 256 }, false, ["encrypt"]);
    const cipher = new Uint8Array(await crypto.subtle.encrypt(
      { name: "AES-GCM", iv: nonce, additionalData: head }, key, bytes));
    const out = new Uint8Array(40 + cipher.length);
    out.set(head, 0);
    out.set(cipher, 40);
    return out;
  }

  // createEventStream owns the admin UI's /events subscription: one live
  // socket, one scheduled reconnect, ever. The naive
  // `onerror -> setTimeout(connect, 3000)` pattern stacks sockets — the
  // browser retries on its own while the handler opens another stream per
  // error, so a server restart multiplies live EventSources (measured 64 in
  // 10s). `open` and the timers are injectable so node:test can pin the
  // lifecycle without a browser.
  function createEventStream(open, handlers, timers) {
    handlers = handlers || {};
    timers = timers || { setTimeout: setTimeout, clearTimeout: clearTimeout };
    var source = null;
    var timer = null;
    var stopped = false;

    function status(name) {
      if (handlers.onStatus) handlers.onStatus(name);
    }

    function teardown() {
      if (timer !== null) {
        timers.clearTimeout(timer);
        timer = null;
      }
      if (source !== null) {
        var dying = source;
        source = null;
        dying.onmessage = null;
        dying.onerror = null;
        dying.onopen = null;
        try { dying.close(); } catch (e) {}
      }
    }

    function start() {
      if (stopped) return;
      teardown();
      status("connecting");
      var s = open();
      source = s;
      s.onopen = function () {
        if (source === s) status("open");
      };
      s.onmessage = function (ev) {
        if (source === s && handlers.onMessage) handlers.onMessage(ev.data);
      };
      s.onerror = function () {
        // Only the live socket may schedule a reconnect; a late callback
        // from an already-replaced stream is ignored.
        if (stopped || source !== s) return;
        teardown();
        status("reconnecting");
        if (timer === null) {
          timer = timers.setTimeout(function () {
            timer = null;
            start();
          }, 3000);
        }
      };
    }

    return {
      start: start,
      stop: function () {
        stopped = true;
        teardown();
      },
    };
  }

  // debounce coalesces a burst of calls into one trailing invocation — the
  // subtitle stream otherwise writes localStorage and refetches panels once
  // per sentence. The returned function carries flush() for page-unload.
  function debounce(fn, ms, timers) {
    timers = timers || { setTimeout: setTimeout, clearTimeout: clearTimeout };
    var timer = null;
    var last = null;
    var wrapped = function () {
      last = arguments;
      if (timer !== null) timers.clearTimeout(timer);
      timer = timers.setTimeout(function () {
        timer = null;
        var args = last;
        last = null;
        fn.apply(null, args);
      }, ms);
    };
    wrapped.flush = function () {
      if (timer === null) return;
      timers.clearTimeout(timer);
      timer = null;
      var args = last;
      last = null;
      fn.apply(null, args);
    };
    wrapped.cancel = function () {
      if (timer !== null) timers.clearTimeout(timer);
      timer = null;
      last = null;
    };
    return wrapped;
  }

  // tabTarget is the focus trap's decision: which element Tab must be sent to
  // inside a modal overlay, or null to let the browser move normally.
  function tabTarget(focusable, active, shift) {
    if (!focusable || !focusable.length) return null;
    var first = focusable[0];
    var last = focusable[focusable.length - 1];
    var inside = active != null && focusable.indexOf(active) !== -1;
    if (shift) {
      return (!inside || active === first) ? last : null;
    }
    return (!inside || active === last) ? first : null;
  }

  return { errorText, buildPairLink, fmtDuration, needsRestart, friendlyAsrError, sealDiagPack, createEventStream, debounce, tabTarget };
});
