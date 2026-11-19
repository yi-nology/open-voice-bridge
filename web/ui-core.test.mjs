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

// ---- createEventStream: the /events lifecycle ----------------------------
// The naive `onerror -> setTimeout(connect, 3000)` pattern stacks sockets:
// the browser retries on its own while the handler opens another stream per
// error, so one server restart multiplies live EventSources (measured 64 in
// 10s, 512 in 18s). These tests pin the single-connection contract.

function manualTimers() {
  let queue = [];
  return {
    setTimeout(fn, ms) { const task = { fn, ms }; queue.push(task); return task; },
    clearTimeout(task) { queue = queue.filter((item) => item !== task); },
    pending() { return queue.length; },
    fire() { const due = queue; queue = []; due.forEach((task) => task.fn()); },
  };
}

function fakeStreamFactory() {
  const created = [];
  return {
    created,
    open() {
      const s = { closed: false, onmessage: null, onerror: null, onopen: null, close() { this.closed = true; } };
      created.push(s);
      return s;
    },
  };
}

test("createEventStream closes the dead socket before reconnecting", () => {
  const { createEventStream } = require("./ui-core.js");
  const factory = fakeStreamFactory();
  const timers = manualTimers();
  const statuses = [];
  const stream = createEventStream(factory.open, { onStatus: (s) => statuses.push(s) }, timers);
  stream.start();
  assert.equal(factory.created.length, 1);
  factory.created[0].onerror();
  assert.equal(factory.created[0].closed, true, "the failed stream must be closed so the browser stops its own retry");
  assert.equal(statuses.at(-1), "reconnecting");
});

test("createEventStream schedules exactly one reconnect under error storms", () => {
  const { createEventStream } = require("./ui-core.js");
  const factory = fakeStreamFactory();
  const timers = manualTimers();
  const stream = createEventStream(factory.open, {}, timers);
  stream.start();
  // an error storm = many callbacks for the same dead socket (queued events,
  // the browser's own retry loop before close lands); capture the handler
  // because teardown nulls the field after the first one runs.
  const storm = factory.created[0].onerror;
  for (let i = 0; i < 50; i += 1) storm();
  assert.equal(timers.pending(), 1, "an error storm must not stack reconnect timers");
  assert.equal(factory.created.length, 1, "no new stream until the one timer fires");
  timers.fire();
  assert.equal(factory.created.length, 2, "the timer opens exactly one replacement");
});

test("createEventStream stop() cancels the pending reconnect", () => {
  const { createEventStream } = require("./ui-core.js");
  const factory = fakeStreamFactory();
  const timers = manualTimers();
  const stream = createEventStream(factory.open, {}, timers);
  stream.start();
  factory.created[0].onerror();
  stream.stop();
  timers.fire();
  assert.equal(factory.created.length, 1, "a stopped stream never resurrects");
  assert.equal(factory.created[0].closed, true);
});

test("createEventStream passes messages and reports the open state", () => {
  const { createEventStream } = require("./ui-core.js");
  const factory = fakeStreamFactory();
  const timers = manualTimers();
  const messages = [];
  const statuses = [];
  const stream = createEventStream(factory.open, { onMessage: (d) => messages.push(d), onStatus: (s) => statuses.push(s) }, timers);
  stream.start();
  factory.created[0].onopen();
  factory.created[0].onmessage({ data: '{"event":"key"}' });
  assert.deepEqual(messages, ['{"event":"key"}']);
  assert.deepEqual(statuses, ["connecting", "open"]);
});

test("createEventStream ignores late errors from a replaced socket", () => {
  const { createEventStream } = require("./ui-core.js");
  const factory = fakeStreamFactory();
  const timers = manualTimers();
  const stream = createEventStream(factory.open, {}, timers);
  stream.start();
  const first = factory.created[0];
  const staleError = first.onerror;
  first.onerror();
  timers.fire();
  assert.equal(factory.created.length, 2);
  // a stale callback from the replaced socket must not schedule anything
  // (teardown nulled the field, so invoke the captured handler directly)
  staleError();
  assert.equal(timers.pending(), 0, "stale errors are ignored");
  assert.equal(factory.created.length, 2);
});

// debounce coalesces bursts (a live subtitle stream writes localStorage and
// refetches panels once per sentence otherwise). Timers are injectable so
// the suite stays instant and deterministic.
test("debounce runs once per quiet gap, with flush when it matters", () => {
  const { debounce } = require("./ui-core.js");
  const timers = manualTimers();
  const calls = [];
  const fn = debounce((v) => calls.push(v), 500, timers);

  fn("a");
  fn("b");
  fn("c");
  assert.deepEqual(calls, [], "nothing runs before the gap");
  assert.equal(timers.pending(), 1, "a burst keeps exactly one timer");
  timers.fire();
  assert.deepEqual(calls, ["c"], "the last call wins the trailing edge");

  fn("d");
  fn.flush();
  assert.deepEqual(calls, ["c", "d"], "flush runs the pending call now");
  assert.equal(timers.pending(), 0);
  fn.flush();
  assert.deepEqual(calls, ["c", "d"], "flush with nothing pending is a no-op");
});

test("debounce cancel drops the pending call", () => {
  const { debounce } = require("./ui-core.js");
  const timers = manualTimers();
  const calls = [];
  const fn = debounce((v) => calls.push(v), 500, timers);
  fn("gone");
  fn.cancel();
  timers.fire();
  assert.deepEqual(calls, []);
  fn.flush();
  assert.deepEqual(calls, [], "a cancelled call never lands");
});
