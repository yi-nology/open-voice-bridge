// node --test ui-core.test.mjs — the pure web helpers' unit tests (波 3 JS
// 测试基建). Run from web/; the Go side bridges via web/js_test.go.
import test from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { errorText, buildPairLink, fmtDuration, needsRestart, friendlyAsrError, sealDiagPack } = require("./ui-core.js");

test("errorText prefers message, then error, then output", () => {
  assert.equal(errorText({ message: "a", error: "b" }, "fb"), "a");
  assert.equal(errorText({ error: "b", output: "c" }, "fb"), "b");
  assert.equal(errorText({ output: "c" }, "fb"), "c");
  assert.equal(errorText({}, "fb"), "fb");
  assert.equal(errorText(null, "fb"), "fb");
  assert.equal(errorText(null), "");
});

test("buildPairLink appends the #pair= fragment", () => {
  assert.equal(
    buildPairLink({ link: "http://10.0.0.5:8788/phone", code: "123456" }, "ignored"),
    "http://10.0.0.5:8788/phone#pair=123456");
  assert.equal(
    buildPairLink({ scheme: "https", port: "9443", code: "000111" }, "box.local"),
    "https://box.local:9443/phone#pair=000111");
  assert.equal(buildPairLink({ code: "222333" }, "box.local"), "https://box.local:8788/phone#pair=222333");
});

test("friendlyAsrError turns the known failures into plain speech", () => {
  assert.match(friendlyAsrError('no ASR provider could be built: primary ASR "mimo": read MiMo API key: open /x/api_key: no such file or directory'), /密钥文件/);
  assert.match(friendlyAsrError("read whisper binary: no such file or directory"), /本地识别程序/);
  assert.match(friendlyAsrError("asr HTTP 401 unauthorized"), /401/);
  assert.match(friendlyAsrError("asr request: dial tcp 127.0.0.1:8080: connect: connection refused"), /连不上/);
  assert.match(friendlyAsrError("no ASR provider could be built"), /识别引擎/);
  assert.equal(friendlyAsrError("怪错误原文"), "怪错误原文");
  assert.equal(friendlyAsrError(""), "");
  assert.equal(friendlyAsrError(null), "");
});

test("needsRestart reads the restart contract in both payload shapes", () => {
  assert.equal(needsRestart({ ok: true, restart_required: true }), true);
  assert.equal(needsRestart({ restart_required: "true" }), true);
  assert.equal(needsRestart({ ok: true }), false);
  assert.equal(needsRestart({ restart_required: false }), false);
  assert.equal(needsRestart(null), false);
  assert.equal(needsRestart(undefined), false);
});

test("fmtDuration rounds to the spoken units", () => {
  assert.equal(fmtDuration(0), "0 秒");
  assert.equal(fmtDuration(45_000), "45 秒");
  assert.equal(fmtDuration(65_000), "1 分 5 秒");
  assert.equal(fmtDuration(120_000), "2 分");
  assert.equal(fmtDuration(3_600_000), "1 小时 0 分");
  assert.equal(fmtDuration(5_400_000), "1 小时 30 分");
});

// openPack mirrors internal/diagbox.Open so the suite pins the wire format
// and the AAD binding the Go CLI depends on.
async function openPack(pack, passphrase, withAAD) {
  const enc = new TextEncoder();
  const view = new DataView(pack.buffer, pack.byteOffset, pack.byteLength);
  const salt = pack.slice(8, 24);
  const nonce = pack.slice(24, 36);
  const head = pack.slice(0, 40);
  const iterations = view.getUint32(36);
  const baseKey = await crypto.subtle.importKey("raw", enc.encode(passphrase), "PBKDF2", false, ["deriveKey"]);
  const key = await crypto.subtle.deriveKey(
    { name: "PBKDF2", hash: "SHA-256", salt, iterations },
    baseKey, { name: "AES-GCM", length: 256 }, false, ["decrypt"]);
  const params = { name: "AES-GCM", iv: nonce };
  if (withAAD) params.additionalData = head;
  return new Uint8Array(await crypto.subtle.decrypt(params, key, pack.slice(40)));
}

test("sealDiagPack emits the OVBDIA1 wire format", async () => {
  const plain = new TextEncoder().encode("诊断摘要，已脱敏");
  const pack = await sealDiagPack(plain, "pass-1");
  assert.equal(new TextDecoder().decode(pack.slice(0, 8)), "OVBDIA1\n");
  assert.equal(new DataView(pack.buffer).getUint32(36), 600000);
  assert.equal(pack.length, 40 + plain.length + 16);
});

test("sealDiagPack round-trips only with the header as additional data", async () => {
  const plain = new TextEncoder().encode(JSON.stringify({ ok: true, note: "跨语言对拍 ✓" }));
  const pack = await sealDiagPack(plain, "测试口令-42");
  const out = await openPack(pack, "测试口令-42", true);
  assert.deepEqual(Array.from(out), Array.from(plain));
  await assert.rejects(() => openPack(pack, "测试口令-42", false), "AAD-less open must fail (Go authenticates the header)");
  await assert.rejects(() => openPack(pack, "wrong", true), "wrong passphrase must fail");
});
