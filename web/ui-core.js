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

  return { errorText, buildPairLink, fmtDuration, sealDiagPack };
});
