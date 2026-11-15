// Pure helpers live in ui-core.js (shared with the node:test suite).
const { errorText, buildPairLink, fmtDuration, needsRestart, sealDiagPack } = XiaoyaoCore;

const KEYS = [
  {key: "power",      chip: "电源", name: "电源键",   hid: "0x66", def: "esc"},
  {key: "up",         chip: "上",   name: "上键",     hid: "0x52", def: "native"},
  {key: "left",       chip: "左",   name: "左键",     hid: "0x50", def: "native"},
  {key: "enter",      chip: "OK",   name: "确定键",   hid: "0x28", def: "native"},
  {key: "right",      chip: "右",   name: "右键",     hid: "0x4F", def: "native"},
  {key: "down",       chip: "下",   name: "下键",     hid: "0x51", def: "native"},
  {key: "back",       chip: "返回", name: "返回键",   hid: "0xF1", def: "esc"},
  {key: "volumeup",   chip: "+",    name: "音量 +",   hid: "0x80", def: "native"},
  {key: "home",       chip: "主页", name: "主页键",   hid: "0x4A", def: "macro(leftalt+tab)"},
  {key: "volumedown", chip: "−",    name: "音量 −",   hid: "0x81", def: "native"},
  {key: "compose",    chip: "菜单", name: "菜单键",   hid: "0x65", def: "native"},
  {key: "grave",      chip: "TV",   name: "TV 键",    hid: "0x35", def: "noop"},
  {key: "f5",         chip: "语音", name: "语音键",   hid: "0x3E", def: "f24"},
];

const ACTION_GROUPS = [
  {label: "键盘", items: [
    ["native", "原生（不映射）"], ["esc", "Escape"], ["enter", "Return"],
    ["tab", "Tab"], ["space", "空格"], ["backspace", "Backspace"],
    ["delete", "Delete"], ["leftmeta", "Super / Win"], ["compose", "菜单键"],
  ]},
  {label: "组合键", items: [
    ["macro(leftalt+tab)", "Alt + Tab（切窗口）"],
    ["macro(leftctrl+c)", "Ctrl + C"], ["macro(leftctrl+v)", "Ctrl + V"],
    ["macro(leftctrl+z)", "Ctrl + Z"],
    ["macro(leftctrl+shift+p)", "Ctrl + Shift + P"],
    ["macro(leftmeta+d)", "Super + D"],
  ]},
  {label: "常用语", items: [
    ["phrase1", "常用语 1（按下插入）"], ["phrase2", "常用语 2（按下插入）"],
    ["phrase3", "常用语 3（按下插入）"], ["phrase4", "常用语 4（按下插入）"],
    ["phrase5", "常用语 5（按下插入）"], ["phrase6", "常用语 6（按下插入）"],
    ["phrase7", "常用语 7（按下插入）"], ["phrase8", "常用语 8（按下插入）"],
    ["phrase-picker", "常用语选单（打开选句浮层）"],
  ]},
  {label: "媒体 / 其他", items: [
    ["playpause", "播放 / 暂停"], ["volumeup", "系统音量 +"],
    ["volumedown", "系统音量 −"], ["mute", "静音"],
    ["noop", "无动作"], ["f24", "F24（语音键专用）"],
  ]},
];

const DEFAULTS = Object.fromEntries(KEYS.map(item => [item.key, item.def]));
const SAFE_OFF = Object.fromEntries(KEYS.map(item => [item.key, "native"]));
SAFE_OFF.power = "esc";
SAFE_OFF.f5 = "f24";

let mapping = {...DEFAULTS};
let enabled = true;
let capturing = false;
let selectedKey = null;
let saveTimer = null;
let dirty = false;
let saveBusy = false;
let asrOptionsLoaded = false;
let asrProviderSignature = "";
let keymapProfileSignature = "";
let subtitleFont = 32;
try {
  subtitleFont = Number(localStorage.getItem("xiaomiSubtitleFont")) || 32;
} catch (error) {}
let deviceItems = [];
let deviceProfiles = [];
let editingDeviceId = "";
let deviceFormInitialized = false;
const subtitleKeys = new Set();
const subtitleLinesEl = document.getElementById("subtitle-lines");
const subtitleEmpty = document.getElementById("subtitle-empty");
const subtitleStage = document.getElementById("subtitle-stage");
const subtitleScene = document.getElementById("subtitle-scene");

const selects = {};

const NATIVE_LABEL = {
  power: "原生（关机风险）", up: "方向上", down: "方向下", left: "方向左",
  right: "方向右", enter: "Return", back: "原生返回",
  volumeup: "系统音量 +", home: "原生主页", volumedown: "系统音量 −",
  compose: "菜单键", grave: "原生 TV", f5: "原生 F5",
};

function optionList(select, value, key) {
  for (const group of ACTION_GROUPS) {
    const og = document.createElement("optgroup");
    og.label = group.label;
    for (const [val, label] of group.items) {
      const option = document.createElement("option");
      option.value = val;
      option.textContent = val === "native" ? (NATIVE_LABEL[key] || label) : label;
      if (val === value) option.selected = true;
      og.appendChild(option);
    }
    select.appendChild(og);
  }
}

function renderKeyList() {
  const list = document.getElementById("key-list");
  list.innerHTML = "";
  for (const item of KEYS) {
    const row = document.createElement("div");
    row.className = "key-row";
    row.dataset.key = item.key;

    const chip = document.createElement("div");
    chip.className = "key-chip";
    chip.textContent = item.chip;

    const meta = document.createElement("div");
    meta.className = "key-meta";
    const name = document.createElement("div");
    name.className = "key-name";
    name.textContent = item.name;
    const hid = document.createElement("div");
    hid.className = "key-hid";
    hid.textContent = "HID " + item.hid;
    meta.append(name, hid);

    const select = document.createElement("select");
    select.dataset.key = item.key;
    optionList(select, mapping[item.key] || item.def, item.key);
    select.addEventListener("change", () => {
      mapping[item.key] = select.value;
      dirty = true;
      scheduleSave();
      selectKey(item.key, false);
    });
    selects[item.key] = select;

    row.append(chip, meta, select);
    row.addEventListener("click", (event) => {
      if (event.target === select) return;
      selectKey(item.key, true);
    });
    list.appendChild(row);
  }
}

function selectKey(key, scroll) {
  selectedKey = key;
  document.querySelectorAll(".key-row").forEach(row => {
    row.classList.toggle("selected", row.dataset.key === key);
  });
  document.querySelectorAll("#remote [data-key]").forEach(button => {
    button.classList.toggle("selected", button.dataset.key === key);
  });
  if (scroll) {
    const row = document.querySelector(`.key-row[data-key="${key}"]`);
    if (row) row.scrollIntoView({block: "nearest", behavior: "smooth"});
  }
}

function setEnabledState(on) {
  enabled = on;
  document.getElementById("enabled").checked = on;
  for (const key in selects) selects[key].disabled = !on;
  const right = document.getElementById("status-right");
  if (right) {
    right.textContent = on
      ? "按键状态：修改后自动保存"
      : "按键状态：自定义映射已关闭（电源仍为 Esc，语音键保持 F24）";
  }
}

function scheduleSave() {
  clearTimeout(saveTimer);
  document.getElementById("save-status").textContent = "修改待保存…";
  document.getElementById("save-status").className = "save-status";
  saveTimer = setTimeout(applyMapping, 600);
}

function collectMapping() {
  if (!enabled) return {...SAFE_OFF};
  return collectMappingValues();
}

function collectMappingValues() {
  const result = {};
  for (const item of KEYS) {
    result[item.key] = selects[item.key] ? selects[item.key].value : item.def;
  }
  return result;
}

async function applyMapping() {
  if (!dirty && !saveBusy) return;
  if (capturing) {
    document.getElementById("save-status").textContent = "捕获中，结束后会自动保存";
    return;
  }
  if (saveBusy) return;
  saveBusy = true;
  document.getElementById("save-status").textContent = "保存中…";
  document.getElementById("save-status").className = "save-status";
  try {
    const res = await fetch("/api/apply", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({mapping: collectMapping()}),
    });
    const data = await res.json();
    if (data.ok) {
      dirty = false;
      document.getElementById("save-status").textContent = "已保存并生效";
      document.getElementById("save-status").className = "save-status ok";
      const preview = document.getElementById("config-preview");
      preview.textContent = data.config || "";
      document.getElementById("perm-config").textContent = data.config || "";
      localStorage.setItem("xiaomiMapping", JSON.stringify(mapping));
      localStorage.setItem("xiaomiEnabled", enabled ? "1" : "0");
      loadStatus(false);
    } else {
      document.getElementById("save-status").textContent = "保存失败：" + errorText(data, "未知错误");
      document.getElementById("save-status").className = "save-status err";
    }
  } catch (error) {
    document.getElementById("save-status").textContent = "保存失败：" + error;
    document.getElementById("save-status").className = "save-status err";
  } finally {
    saveBusy = false;
  }
}

function resetDefaults() {
  mapping = {...DEFAULTS};
  for (const item of KEYS) {
    if (selects[item.key]) selects[item.key].value = mapping[item.key];
  }
  dirty = true;
  scheduleSave();
  setEnabledState(true);
}

function setCaptureUi(active) {
  capturing = active;
  document.getElementById("capture-btn").textContent = active ? "结束捕获" : "开始捕获";
  document.getElementById("capture-btn").className = active ? "btn small primary" : "btn small";
  document.getElementById("capture-banner").classList.toggle("show", active);
  document.getElementById("log").classList.toggle("show", active);
  const micChip = document.getElementById("chip-mic");
  if (micChip) micChip.classList.toggle("warn", active);
  const right = document.getElementById("status-right");
  if (right && active) right.textContent = "按键状态：捕获中 · 注入已暂停";
}

async function toggleCapture() {
  const url = capturing ? "/api/capture/stop" : "/api/capture/start";
  const res = await fetch(url, {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: "{}",
  });
  const data = await res.json();
  if (data.ok) {
    setCaptureUi(!capturing);
    if (!capturing && dirty) scheduleSave();
  } else {
    document.getElementById("save-status").textContent = "捕获失败：" + errorText(data, "未知错误");
    document.getElementById("save-status").className = "save-status err";
  }
}

function highlight(key, down) {
  document.querySelectorAll(`#remote [data-key="${key}"]`).forEach(remoteButton => {
    remoteButton.classList.toggle("pressed", down);
  });
  const row = document.querySelector(`.key-row[data-key="${key}"]`);
  if (row) row.classList.toggle("pressed", down);
  if (down) {
    selectKey(key, true);
  }
}

function logKey(key, value) {
  const box = document.getElementById("log");
  const item = KEYS.find(entry => entry.key === key);
  const row = document.createElement("div");
  const time = new Date().toLocaleTimeString("zh-CN", {hour12: false});
  row.className = value ? "down" : "";
  row.textContent = `${time}  ${item ? item.name : key}  ${value ? "按下" : "松开"}`;
  box.prepend(row);
  while (box.children.length > 12) box.removeChild(box.lastChild);
}

function setChip(id, text, cls) {
  const chip = document.getElementById(id);
  if (!chip) return;
  chip.textContent = text;
  chip.classList.remove("good", "bad", "warn");
  if (cls) chip.classList.add(cls);
}

function remoteConnectedOf(status) {
  return !!(status.remote_present || status.ble_connected);
}

function mapStateOf(status) {
  if (status.platform === "darwin") {
    // evdev/uinput is Linux-only; on macOS the remote's own HID drives the
    // keys, so "mapping unavailable" is the normal state, not a fault.
    return status.capturing ? "捕获中" : "原生 HID";
  }
  return status.capturing ? "捕获中"
    : status.keyd_active ? "映射运行中"
    : status.helper_error ? "映射不可用"
    : "映射未运行";
}

function updateConnection(status) {
  lastStatus = status;
  renderAttention(status);
  loadInputsOverview();
  const remoteConnected = remoteConnectedOf(status);
  const mapState = mapStateOf(status);
  // macOS ships no virtual microphone by design (docs/design/macos-compat.md):
  // the ASR chain consumes the decoded stream directly, so an unhealthy sink
  // is the normal state there and must not read as a fault.
  const isMac = status.platform === "darwin";
  const remote = document.getElementById("conn-remote");
  remote.textContent = remoteConnected ? "已连接" : "未连接";
  remote.className = "value " + (remoteConnected ? "good" : "bad");
  document.getElementById("conn-address").textContent = status.address || "";
  const keyd = document.getElementById("conn-keyd");
  const keydSub = document.getElementById("conn-keyd-sub");
  if (isMac) {
    keyd.textContent = "不适用（macOS 原生 HID）";
    keyd.className = "value";
    if (keydSub) keydSub.textContent = "macOS 无 evdev/uinput，按键走遥控器原生 HID";
  } else {
    keyd.textContent = mapState;
    keyd.className = "value " + (status.keyd_active ? "good" : "warn");
    if (keydSub) keydSub.textContent = "Go remapper；捕获时暂停，结束后自动恢复";
  }
  const micHealthy = status.mic_healthy !== false;
  const mic = document.getElementById("conn-mic");
  const micSub = document.getElementById("conn-mic-sub");
  if (isMac) {
    mic.textContent = status.mic_active ? "直采模式" : "未连接";
    mic.className = "value " + (status.mic_active ? "good" : "bad");
    if (micSub) micSub.textContent = "ASR 直采（macOS 无虚拟麦）";
  } else {
    mic.textContent = status.mic_active ? (micHealthy ? "运行中" : "节点恢复中") : "未运行";
    mic.className = "value " + (status.mic_active && micHealthy ? "good" : (status.mic_active ? "warn" : "bad"));
    if (micSub) micSub.textContent = "xiaoyao_mic → PipeWire";
  }
  const asr = document.getElementById("conn-asr");
  asr.textContent = status.asr_backend || "未知";
  asr.className = "value";
  const last = document.getElementById("conn-last");
  if (last) {
    if (status.asr_error) {
      last.textContent = status.asr_error;
      last.className = "sub err";
    } else {
      const ago = fmtAgo(status.last_at);
      last.textContent = status.last_text
        ? "最近识别" + (ago ? "（" + ago + "）" : "") + "：" + status.last_text
        : "语音输入前请先点进目标输入框";
      last.className = "sub";
    }
  }
  const asrSelect = document.getElementById("asr-select");
  if (asrSelect && Array.isArray(status.asr_providers)) {
    const signature = status.asr_providers.join(",");
    if (!asrOptionsLoaded || signature !== asrProviderSignature) {
      asrSelect.innerHTML = "";
      for (const name of status.asr_providers) {
        const option = document.createElement("option");
        option.value = name;
        option.textContent = name;
        asrSelect.appendChild(option);
      }
      asrOptionsLoaded = true;
      asrProviderSignature = signature;
    }
    if (status.asr_provider) asrSelect.value = status.asr_provider;
  }
  const profileSelect = document.getElementById("keymap-profile");
  if (profileSelect && Array.isArray(status.keymap_profiles)) {
    const signature = status.keymap_profiles.join(",");
    if (signature !== keymapProfileSignature) {
      profileSelect.innerHTML = "";
      for (const name of status.keymap_profiles) {
        const option = document.createElement("option");
        option.value = name;
        option.textContent = name === "custom" ? "自定义" : name;
        profileSelect.appendChild(option);
      }
      keymapProfileSignature = signature;
    }
    if (status.keymap_profile) profileSelect.value = status.keymap_profile;
  }
  const keysValue = document.getElementById("stats-keys");
  if (keysValue) {
    keysValue.textContent = (status.key_total || 0).toLocaleString();
    const top = Object.entries(status.key_counts || {}).sort((a, b) => b[1] - a[1]).slice(0, 3);
    const topSub = document.getElementById("stats-keys-top");
    if (topSub) {
      topSub.textContent = top.length
        ? "最常用：" + top.map(([name, count]) => name + " " + count).join(" · ")
        : "累计按下";
    }
  }
  const autostart = document.getElementById("conn-autostart");
  if (autostart) {
    const autostartSub = document.getElementById("conn-autostart-sub");
    if (isMac) {
      autostart.textContent = "启动方式：手动";
      autostart.className = "value warn";
      if (autostartSub) autostartSub.textContent = "macOS 用 launchd 或终端手动运行 agent";
    } else {
      autostart.textContent =
        "agent " + (status.agent_autostart ? "已启用" : "未启用") +
        " · remap " + (status.remap_autostart ? "已启用" : "未启用");
      autostart.className = "value " + (status.agent_autostart && status.remap_autostart ? "good" : "bad");
      if (autostartSub) autostartSub.textContent = "systemd 用户 agent + 系统 remap 服务";
    }
  }
  setChip("chip-remote", remoteConnected ? "遥控器已连接" : "遥控器未连接",
          remoteConnected ? "good" : "bad");
  setChip("chip-keyd", mapState,
          status.keyd_active ? "good" : "warn");
  setChip("chip-mic",
    status.mic_active ? ((micHealthy || status.atvv_ready) ? "语音就绪" : "音频节点异常") : "语音未运行",
    status.mic_active ? ((micHealthy || status.atvv_ready) ? "good" : "warn") : "bad");
  // OPS-06: readiness is "can dictation work" — the ASR chain is the core;
  // the remote, the remapper and the virtual mic are optional gaps (see the
  // quickstart groups) and must not hold the verdict hostage.
  const coreReady = !!(!status.asr_error && status.asr_provider);
  const guidedState = document.getElementById("guided-state");
  const guidedAsr = document.getElementById("guided-asr");
  const guidedMeta = document.getElementById("guided-meta");
  // Three-state verdict: the chain must be ready AND some input source must
  // be reachable, or the green "语音就绪" contradicts a red 未连接 beside it.
  const phoneData = lastInputs || {};
  const phoneLive = !!phoneData.enabled && (phoneData.sources || []).some(item => item.kind === "phone");
  const hasSource = remoteConnected || phoneLive;
  const verdict = !coreReady ? "待检查" : (hasSource ? "语音就绪" : "待连接");
  if (guidedState) guidedState.textContent = verdict;
  const heroDot = document.getElementById("hero-dot");
  if (heroDot) heroDot.className = "hero-dot" + (coreReady && hasSource ? "" : (coreReady ? " warn" : " bad"));
  if (guidedAsr) guidedAsr.textContent = status.asr_error ? "需检查" : ((status.asr_backend || "未知").split(" ")[0] || "未知");
  if (guidedMeta) {
    const latency = Number(status.last_latency_ms) || 0;
    guidedMeta.textContent = status.asr_error
      ? "识别配置需检查"
      : ((status.asr_backend || "ASR") + (latency > 0 ? " · " + latency + " ms" : (coreReady ? " · 就绪" : " · 待检查")));
  }
  const gotoDevices = document.getElementById("goto-devices");
  if (gotoDevices) gotoDevices.hidden = remoteConnected;
  const sideStatus = document.getElementById("side-status");
  if (sideStatus) sideStatus.textContent = remoteConnected ? "遥控器在线" : "等待遥控器";
  const sideDot = document.querySelector(".side-foot i");
  if (sideDot) sideDot.style.background = remoteConnected ? "var(--green)" : "var(--orange)";
  document.getElementById("perm-config").textContent = status.config || "";
  const permLinux = document.getElementById("perm-linux");
  const permMac = document.getElementById("perm-mac");
  if (permLinux && permMac) {
    permLinux.hidden = isMac;
    permMac.hidden = !isMac;
  }
  const right = document.getElementById("status-right");
  if (right && enabled) {
    right.textContent = "按键状态：" + mapState +
      " · " + (remoteConnected ? "遥控器已连接" : "遥控器未连接");
  }
  if (status.capturing && !capturing) setCaptureUi(true);
  if (!status.capturing && capturing) setCaptureUi(false);
}

async function loadStatus(initial, syncMapping) {
  try {
    const res = await fetch("/api/status");
    const status = await res.json();
    updateConnection(status);
    if (syncMapping && status.mapping) {
      mapping = {...DEFAULTS, ...status.mapping};
      for (const key of Object.keys(DEFAULTS)) {
        if (selects[key]) selects[key].value = mapping[key] || DEFAULTS[key];
      }
      dirty = false;
      document.getElementById("save-status").textContent = "已应用预设";
      document.getElementById("save-status").className = "save-status ok";
    }
    if (initial) {
      mapping = {...DEFAULTS, ...(status.mapping || {})};
      const safe = JSON.stringify(SAFE_OFF);
      const current = JSON.stringify({...DEFAULTS, ...(status.mapping || {})});
      const storedMapping = localStorage.getItem("xiaomiMapping");
      const storedEnabled = localStorage.getItem("xiaomiEnabled");
      enabled = storedEnabled === null ? current !== safe : storedEnabled === "1";
      if (storedMapping && enabled) {
        try {
          const parsed = JSON.parse(storedMapping);
          for (const key in parsed) if (key in DEFAULTS) mapping[key] = parsed[key];
        } catch (error) {}
      }
      renderKeyList();
      setEnabledState(enabled);
      if (enabled) {
        const right = document.getElementById("status-right");
        if (right) {
          right.textContent = "按键状态：" + mapStateOf(status) +
            " · " + (remoteConnectedOf(status) ? "遥控器已连接" : "遥控器未连接");
        }
      }
      if (status.last_text && subtitleLinesEl && subtitleLinesEl.children.length === 0) {
        addSubtitle(status.last_text, "", {dedupeText: true});
      }
      selectKey("enter", false);
      const preview = document.getElementById("config-preview");
      preview.textContent = status.config || "";
    }
  } catch (error) {
    setChip("chip-keyd", "无法连接助手", "bad");
  }
}

function subtitleKey(text, at) {
  return (at || "") + "|" + text;
}

function subtitleTime(at) {
  const date = at ? new Date(at) : new Date();
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleTimeString("zh-CN", {hour12: false});
}

function applySubtitleFont() {
  if (subtitleStage) subtitleStage.style.setProperty("--subtitle-size", subtitleFont + "px");
  try { localStorage.setItem("xiaomiSubtitleFont", String(subtitleFont)); } catch (error) {}
}

function persistSubtitles() {
  if (!subtitleLinesEl) return;
  const items = [...subtitleLinesEl.querySelectorAll(".subtitle-line")].slice(-80).map(line => ({
    text: line.querySelector(".subtitle-text")?.textContent || "",
    at: line.dataset.at || "",
  }));
  try { localStorage.setItem("xiaomiSubtitles", JSON.stringify(items)); } catch (error) {}
}

// 清空/复制 only make sense with lines on the stage (UX 验收 B#8).
function syncSubtitleActions() {
  const empty = !subtitleLinesEl || subtitleLinesEl.children.length === 0;
  for (const id of ["subtitle-clear", "subtitle-copy"]) {
    const btn = document.getElementById(id);
    if (btn) btn.disabled = empty;
  }
}

function addSubtitle(text, at, options = {}) {
  if (!subtitleLinesEl) return;
  text = (text || "").trim();
  if (!text) return;
  const key = subtitleKey(text, at);
  if (subtitleKeys.has(key)) return;
  if (options.dedupeText) {
    const last = subtitleLinesEl.lastElementChild;
    if (last && last.querySelector(".subtitle-text")?.textContent === text) return;
  }
  subtitleKeys.add(key);
  const line = document.createElement("div");
  line.className = "subtitle-line";
  if (options.kind) line.classList.add(options.kind);
  line.dataset.key = key;
  line.dataset.at = at || "";
  const body = document.createElement("span");
  body.className = "subtitle-text";
  body.textContent = text;
  const meta = document.createElement("span");
  meta.className = "meta";
  meta.textContent = subtitleTime(at);
  line.append(body, meta);
  subtitleLinesEl.querySelectorAll(".subtitle-line.active").forEach(item => item.classList.remove("active"));
  line.classList.add("active");
  subtitleLinesEl.appendChild(line);
  while (subtitleLinesEl.children.length > 80) {
    const first = subtitleLinesEl.firstElementChild;
    subtitleKeys.delete(first.dataset.key);
    first.remove();
  }
  if (subtitleEmpty) subtitleEmpty.style.display = "none";
  syncSubtitleActions();
  if (options.scroll !== false && document.getElementById("tab-subtitle").classList.contains("active")) {
    line.scrollIntoView({block: "center", behavior: "smooth"});
  }
  persistSubtitles();
}

function loadSubtitles() {
  applySubtitleFont();
  let items = [];
  try { items = JSON.parse(localStorage.getItem("xiaomiSubtitles") || "[]"); } catch (error) {}
  if (!Array.isArray(items)) items = [];
  for (const item of items.slice(-80)) {
    if (item && item.text) addSubtitle(item.text, item.at, {scroll: false});
  }
  const active = subtitleLinesEl?.querySelector(".subtitle-line.active");
  if (active) active.scrollIntoView({block: "center"});
}

function clearSubtitles() {
  clearPartialLine();
  if (!subtitleLinesEl) return;
  subtitleLinesEl.innerHTML = "";
  subtitleKeys.clear();
  if (subtitleEmpty) subtitleEmpty.style.display = "";
  syncSubtitleActions();
  try { localStorage.removeItem("xiaomiSubtitles"); } catch (error) {}
}

function copySubtitles() {
  if (!subtitleLinesEl) return;
  const text = [...subtitleLinesEl.querySelectorAll(".subtitle-text")]
    .map(item => item.textContent).join("\n");
  if (!text) return;
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).catch(() => {});
  }
}

function deviceMessage(text, cls) {
  const node = document.getElementById("device-msg");
  if (!node) return;
  node.textContent = text || "";
  node.className = "msg " + (cls || "");
}

// showRestartBanner raises the one global "needs a service restart" notice
// (restart_required contract). Call it from every save whose reply carries the
// flag; the banner is idempotent and carries the command to run.
function showRestartBanner() {
  let banner = document.getElementById("restart-banner");
  if (!banner) {
    banner = document.createElement("div");
    banner.id = "restart-banner";
    banner.className = "restart-banner";
    banner.innerHTML =
      '<span>部分设置已保存，重启 xiaoyao 服务后生效。</span>' +
      '<button class="btn small" id="restart-copy">复制重启命令</button>' +
      '<button class="btn small" id="restart-dismiss">知道了</button>';
    document.body.appendChild(banner);
    banner.querySelector("#restart-copy").addEventListener("click", () => {
      const command = "systemctl --user restart xiaoyao";
      const done = () => { banner.querySelector("#restart-copy").textContent = "已复制"; };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(command).then(done).catch(() => { window.prompt("复制重启命令：", command); });
      } else {
        window.prompt("复制重启命令：", command);
      }
    });
    banner.querySelector("#restart-dismiss").addEventListener("click", () => banner.remove());
  }
  banner.hidden = false;
}

function subtitleMsg(text, cls) {
  const node = document.getElementById("subtitle-msg");
  if (!node) return;
  node.textContent = text || "";
  node.className = "msg " + (cls || "");
}

function populateDeviceProfiles(selected) {
  const select = document.getElementById("device-profile");
  if (!select) return;
  const current = selected || select.value;
  select.innerHTML = "";
  for (const profile of deviceProfiles) {
    const option = document.createElement("option");
    option.value = profile.id;
    option.textContent = profile.name + " (" + profile.id + ")";
    select.appendChild(option);
  }
  if (current) select.value = current;
}

function renderDevices(items) {
  const list = document.getElementById("device-list");
  if (!list) return;
  list.innerHTML = "";
  if (!items.length) {
    const empty = document.createElement("div");
    empty.className = "note";
    empty.textContent = "还没有配置设备。";
    list.appendChild(empty);
    return;
  }
  for (const item of items) {
    const row = document.createElement("div");
    row.className = "device-row" + (item.active ? " active" : "");
    const main = document.createElement("div");
    main.className = "device-main";
    const name = document.createElement("div");
    name.className = "device-name";
    name.textContent = item.name || item.id;
    if (item.active) {
      const badge = document.createElement("span");
      badge.className = "device-badge";
      badge.textContent = "当前语音";
      name.appendChild(badge);
    }
    if (item.connected) {
      const badge = document.createElement("span");
      badge.className = "device-badge connected";
      badge.textContent = "已连接";
      name.appendChild(badge);
    } else if (item.paired === false) {
      const badge = document.createElement("span");
      badge.className = "device-badge warn";
      badge.textContent = "未配对";
      name.appendChild(badge);
    }
    if (typeof item.battery === "number" && item.battery >= 0) {
      const badge = document.createElement("span");
      badge.className = "device-badge battery";
      badge.textContent = item.battery + "%";
      name.appendChild(badge);
    }
    const meta = document.createElement("div");
    meta.className = "device-meta";
    const profileInfo = deviceProfiles.find(profile => profile.id === item.profile);
    meta.textContent = [item.address, profileInfo?.name || item.profile, (item.capabilities || []).join(" / ")].filter(Boolean).join(" · ");
    main.append(name, meta);
    const actions = document.createElement("div");
    actions.className = "device-actions";
    const edit = document.createElement("button");
    edit.className = "btn small";
    edit.textContent = "编辑";
    edit.addEventListener("click", () => fillDeviceForm(item));
    actions.appendChild(edit);
    if (!item.active && (item.capabilities || []).includes("voice")) {
      const select = document.createElement("button");
      select.className = "btn small primary";
      select.textContent = "设为语音";
      select.addEventListener("click", () => deviceRequest("/api/devices/select", {id: item.id}, "已切换语音设备"));
      actions.appendChild(select);
    }
    if (!item.connected) {
      const connect = document.createElement("button");
      connect.className = "btn small";
      connect.textContent = "连接";
      connect.addEventListener("click", async () => {
        await deviceRequest("/api/devices/connect", {id: item.id, connect: true}, "正在连接设备");
        setTimeout(loadDevices, 1500);
      });
      actions.appendChild(connect);
    } else if (!item.active) {
      const disconnect = document.createElement("button");
      disconnect.className = "btn small";
      disconnect.textContent = "断开";
      disconnect.addEventListener("click", () => deviceRequest("/api/devices/connect", {id: item.id, connect: false}, "已断开设备"));
      actions.appendChild(disconnect);
    }
    const toggle = document.createElement("button");
    toggle.className = "btn small";
    toggle.textContent = item.enabled ? "停用" : "启用";
    if (item.active) toggle.disabled = true;
    toggle.addEventListener("click", () => deviceRequest("/api/devices/toggle", {id: item.id, enabled: !item.enabled}, item.enabled ? "已停用设备" : "已启用设备"));
    actions.appendChild(toggle);
    row.append(main, actions);
    list.appendChild(row);
  }
}

function renderDiscovered(items) {
  const list = document.getElementById("discovered-list");
  const title = document.getElementById("discovered-title");
  if (!list) return;
  list.innerHTML = "";
  // The scan also lists keyboards and headsets (UX 验收 A#10): split the
  // remote-shaped matches from the rest so the remotes never drown.
  const available = items.filter(item => !item.configured);
  const suggest = item => !!(item.profile || (item.capabilities && item.capabilities.length));
  const remotes = available.filter(suggest);
  const others = available.filter(item => !suggest(item));
  if (!available.length) {
    if (title) title.style.display = "none";
    return;
  }
  if (title) title.style.display = "";
  const ordered = [...remotes, ...others];
  let lastGroup = null;
  for (const item of ordered) {
    const group = suggest(item) ? "推荐（遥控器类）" : "其它蓝牙设备";
    if (group !== lastGroup) {
      lastGroup = group;
      const head = document.createElement("div");
      head.className = "note";
      head.style.margin = "8px 0 4px";
      head.textContent = group;
      list.appendChild(head);
    }
    const row = document.createElement("div");
    row.className = "device-row";
    const main = document.createElement("div");
    main.className = "device-main";
    const name = document.createElement("div");
    name.className = "device-name";
    const profileInfo = deviceProfiles.find(profile => profile.id === item.profile);
    name.textContent = profileInfo?.name || item.name || "未知设备";
    if (item.profile) {
      const badge = document.createElement("span");
      badge.className = "device-badge";
      badge.textContent = item.profile;
      name.appendChild(badge);
    }
    if (item.connected) {
      const badge = document.createElement("span");
      badge.className = "device-badge connected";
      badge.textContent = "已连接";
      name.appendChild(badge);
    } else if (item.paired) {
      const badge = document.createElement("span");
      badge.className = "device-badge warn";
      badge.textContent = "已配对未连接";
      name.appendChild(badge);
    } else {
      const badge = document.createElement("span");
      badge.className = "device-badge warn";
      badge.textContent = "未配对";
      name.appendChild(badge);
    }
    if (typeof item.battery === "number" && item.battery >= 0) {
      const badge = document.createElement("span");
      badge.className = "device-badge battery";
      badge.textContent = item.battery + "%";
      name.appendChild(badge);
    }
    const meta = document.createElement("div");
    meta.className = "device-meta";
    meta.textContent = [item.address, item.vendor && item.product ? item.vendor + ":" + item.product : "", (item.capabilities || []).join(" / ")].filter(Boolean).join(" · ");
    main.append(name, meta);
    const actions = document.createElement("div");
    actions.className = "device-actions";
    if (item.configured) {
      const added = document.createElement("span");
      added.className = "note";
      added.textContent = "已配置为遥控器";
      actions.appendChild(added);
    } else {
      const add = document.createElement("button");
      add.className = "btn small primary";
      add.textContent = "添加为遥控器";
      add.addEventListener("click", () => addDiscoveredDevice(item));
      actions.appendChild(add);
    }
    row.append(main, actions);
    list.appendChild(row);
  }
}

async function loadDiscovery() {
  if (!deviceProfiles.length) await loadDevices();
  try {
    const res = await fetch("/api/devices/discover");
    const data = await res.json();
    if (!data.ok) {
      deviceMessage(errorText(data, "扫描失败"), "err");
      return;
    }
    const devices = data.devices || [];
    renderDiscovered(devices);
    if (!devices.length) {
      deviceMessage("未发现已连接的蓝牙输入设备，请先在系统蓝牙中配对连接。", "");
    } else if (devices.every(item => item.configured)) {
      deviceMessage("已连接的设备都已经添加。", "ok");
    }
  } catch (error) {
    deviceMessage("扫描失败：" + error, "err");
  }
}

async function addDiscoveredDevice(item) {
  const profile = item.profile || "generic-ble-hid";
  const suffix = (item.address || "").replace(/:/g, "").slice(-6).toLowerCase();
  const id = profile + "-" + (suffix || Date.now().toString(36));
  const profileInfo = deviceProfiles.find(entry => entry.id === profile);
  await deviceRequest("/api/devices/save", {
    id,
    name: profileInfo?.name || item.name || id,
    address: item.address,
    profile,
    enabled: true,
    vendor: item.vendor || "",
    product: item.product || "",
    name_match: [],
  }, "设备已添加");
  await loadDiscovery();
}

async function loadDevices() {
  try {
    const res = await fetch("/api/devices");
    const data = await res.json();
    if (!data.ok) {
      deviceMessage(errorText(data, "加载设备失败"), "err");
      return;
    }
    deviceItems = data.devices || [];
    deviceProfiles = data.profiles || [];
    populateDeviceProfiles();
    renderDevices(deviceItems);
    syncRemoteFigureToActive();
    if (!deviceFormInitialized) {
      deviceFormInitialized = true;
      resetDeviceForm();
    }
  } catch (error) {
    deviceMessage("加载设备失败：" + error, "err");
  }
}

function fillDeviceForm(item) {
  editingDeviceId = item.id;
  document.getElementById("device-form-title").textContent = "编辑设备 " + item.id;
  document.getElementById("device-id").value = item.id;
  document.getElementById("device-id").disabled = true;
  document.getElementById("device-name").value = item.name || "";
  document.getElementById("device-address").value = item.address || "";
  populateDeviceProfiles(item.profile);
  document.getElementById("device-vendor").value = item.vendor || "";
  document.getElementById("device-product").value = item.product || "";
  document.getElementById("device-names").value = (item.name_match || []).join(", ");
  document.getElementById("device-enabled").checked = !!item.enabled;
  deviceMessage("", "");
}

function resetDeviceForm() {
  editingDeviceId = "";
  document.getElementById("device-form-title").textContent = "添加设备";
  document.getElementById("device-id").value = "";
  document.getElementById("device-id").disabled = false;
  document.getElementById("device-name").value = "";
  document.getElementById("device-address").value = "";
  const preferred = deviceProfiles.find(profile => profile.id === "rc003") || deviceProfiles[0];
  populateDeviceProfiles(preferred?.id);
  document.getElementById("device-vendor").value = "";
  document.getElementById("device-product").value = "";
  document.getElementById("device-names").value = "";
  document.getElementById("device-enabled").checked = true;
  deviceMessage("", "");
}

async function deviceRequest(url, payload, success) {
  deviceMessage("处理中…", "");
  try {
    const res = await fetch(url, {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(payload),
    });
    const data = await res.json();
    if (!data.ok) {
      deviceMessage(errorText(data, "操作失败"), "err");
      return;
    }
    deviceMessage(success, "ok");
    await loadDevices();
    await loadStatus(false);
  } catch (error) {
    deviceMessage("操作失败：" + error, "err");
  }
}

async function saveDevice() {
  const profile = document.getElementById("device-profile").value;
  const address = document.getElementById("device-address").value.trim().toUpperCase();
  let id = editingDeviceId || document.getElementById("device-id").value.trim().toLowerCase();
  if (!profile) {
    deviceMessage("请选择设备 Profile", "err");
    return;
  }
  if (!/^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$/.test(address)) {
    deviceMessage("蓝牙地址格式应为 AA:BB:CC:DD:EE:FF", "err");
    return;
  }
  if (!id) {
    const suffix = address.replace(/:/g, "").slice(-6).toLowerCase();
    id = profile + "-" + suffix;
    document.getElementById("device-id").value = id;
  }
  if (!/^[a-z0-9][a-z0-9_-]{0,31}$/.test(id)) {
    deviceMessage("设备 ID 只能使用小写字母、数字、_ 或 -，最长 32 位", "err");
    return;
  }
  if (!editingDeviceId && deviceItems.some(item => item.id === id)) {
    if (!confirm("设备 ID " + id + " 已存在，覆盖它？")) return;
  }
  const names = document.getElementById("device-names").value
    .split(/[,\n]/).map(item => item.trim()).filter(Boolean);
  const payload = {
    id,
    name: document.getElementById("device-name").value.trim() || id,
    address,
    profile,
    enabled: document.getElementById("device-enabled").checked,
    vendor: document.getElementById("device-vendor").value.trim(),
    product: document.getElementById("device-product").value.trim(),
    name_match: names,
  };
  await deviceRequest("/api/devices/save", payload, "设备已保存");
  if (deviceItems.some(item => item.id === id && item.active)) {
    await deviceRequest("/api/devices/select", {id}, "已应用当前设备");
  }
}

async function deleteDevice() {
  const id = editingDeviceId || document.getElementById("device-id").value.trim();
  if (!id) return;
  if (!confirm("删除设备 " + id + "？")) return;
  await deviceRequest("/api/devices/delete", {id}, "设备已删除");
  resetDeviceForm();
}

// ---- input sources: BLE remote + paired phones ----
let pairCountdownTimer = null;
let pairExpiresAt = 0;

function inputsMessage(text, cls) {
  const node = document.getElementById("phone-pair-hint");
  if (!node) return;
  node.textContent = text || "";
  node.className = "note" + (cls ? " " + cls : "");
}

function fmtAgo(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  if (date.getUTCFullYear() < 2000) return "";
  const diff = Date.now() - date.getTime();
  if (diff < 0) return "";
  if (diff < 20000) return "刚刚";
  if (diff < 120000) return Math.round(diff / 1000) + " 秒前";
  if (diff < 7200000) return Math.round(diff / 60000) + " 分钟前";
  return date.toLocaleTimeString("zh-CN", {hour12: false});
}

function makeBadge(text, cls) {
  const badge = document.createElement("span");
  badge.className = "device-badge " + cls;
  badge.textContent = text;
  return badge;
}

// 手机输入开关（OPS-01）：落盘到 config.phone.enabled，监听器随服务启动，
// 所以保存后提示重启——与配置导入同一口径。
async function setPhoneEnabled(enabled, btn) {
  if (btn) btn.disabled = true;
  try {
    const res = await fetch("/api/phone/enabled", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({enabled: enabled}),
    });
    const data = await res.json();
    if (!data.ok) {
      inputsMessage("保存失败：" + (data.error || res.status), "err");
    } else {
      inputsMessage("已保存，重启服务后生效。", "ok");
      if (needsRestart(data)) showRestartBanner();
    }
  } catch (error) {
    inputsMessage("保存失败：" + error, "err");
  }
  if (btn) btn.disabled = false;
  await loadStatus(false);
  loadInputs();
}

function renderInputs(data) {
  const list = document.getElementById("inputs-list");
  if (!list) return;
  list.innerHTML = "";
  const running = !!data.enabled;
  const saved = !!data.configured;
  const toggleRow = document.createElement("div");
  toggleRow.className = "device-row";
  const toggleMain = document.createElement("div");
  toggleMain.className = "device-main";
  const toggleName = document.createElement("div");
  toggleName.className = "device-name";
  toggleName.textContent = "手机输入";
  toggleName.appendChild(makeBadge(running ? "运行中" : (saved ? "重启后生效" : "未启用"), running ? "connected" : (saved ? "warn" : "")));
  const toggleMeta = document.createElement("div");
  toggleMeta.className = "device-meta";
  toggleMeta.textContent = "手机浏览器当虚拟遥控与外置麦克风；保存后重启服务生效。";
  toggleMain.append(toggleName, toggleMeta);
  const toggleActions = document.createElement("div");
  toggleActions.className = "device-actions";
  const toggleBtn = document.createElement("button");
  toggleBtn.className = "btn small";
  toggleBtn.textContent = saved ? "关闭手机输入" : "启用手机输入";
  toggleBtn.addEventListener("click", () => setPhoneEnabled(!saved, toggleBtn));
  toggleActions.appendChild(toggleBtn);
  toggleRow.append(toggleMain, toggleActions);
  list.appendChild(toggleRow);

  if (!running) {
    const note = document.createElement("div");
    note.className = "note";
    note.textContent = saved
      ? "配置已启用手机输入，重启服务后这里会显示遥控器与已连接手机。"
      : "手机输入未启用（config.phone.enabled = false）。开启后这里会显示遥控器与已连接手机。";
    list.appendChild(note);
    return;
  }
  const sources = data.sources || [];
  if (!sources.length) {
    const note = document.createElement("div");
    note.className = "note";
    note.textContent = "当前没有已注册的输入源。";
    list.appendChild(note);
    return;
  }
  for (const source of sources) {
    const row = document.createElement("div");
    row.className = "device-row";
    const main = document.createElement("div");
    main.className = "device-main";
    const name = document.createElement("div");
    name.className = "device-name";
    const kind = document.createElement("span");
    kind.className = "src-kind " + (source.kind === "phone" ? "phone" : "remote");
    kind.textContent = source.kind === "phone" ? "手机" : "遥控器";
    name.appendChild(kind);
    name.appendChild(document.createTextNode(source.name || source.id || "未命名"));
    if (source.connected) name.appendChild(makeBadge("在线", "connected"));
    if (source.speaking) name.appendChild(makeBadge("正在说话", "connected"));
    const meta = document.createElement("div");
    meta.className = "device-meta";
    meta.textContent = [source.address, source.last_active ? fmtAgo(source.last_active) : ""].filter(Boolean).join(" · ");
    main.append(name, meta);
    row.appendChild(main);
    list.appendChild(row);
  }
}

async function loadInputs() {
  const list = document.getElementById("inputs-list");
  if (!list) return;
  try {
    const res = await fetch("/api/inputs");
    const data = await res.json();
    renderInputs(data || {});
  } catch (error) {
    inputsMessage("加载输入源失败：" + error, "err");
  }
}

function stopPairCountdown() {
  if (pairCountdownTimer) {
    clearInterval(pairCountdownTimer);
    pairCountdownTimer = null;
  }
}

function tickPairCountdown() {
  const remaining = Math.max(0, Math.round((pairExpiresAt - Date.now()) / 1000));
  const node = document.getElementById("pair-countdown");
  if (node) node.textContent = "有效期 " + remaining + " 秒";
  if (remaining <= 0) {
    stopPairCountdown();
    generatePairCode(true);
  }
}

async function generatePairCode(silent) {
  const btn = document.getElementById("phone-pair-btn");
  if (btn) { btn.disabled = true; btn.textContent = silent ? "刷新中…" : "生成中…"; }
  try {
    const res = await fetch("/api/phone/pairing", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: "{}",
    });
    const data = await res.json();
    if (!data.enabled) {
      document.getElementById("pair-code-card").hidden = true;
      stopPairCountdown();
      inputsMessage("手机输入未启用，无法生成配对码。", "warn");
      return;
    }
    if (!data.code) {
      inputsMessage("生成配对码失败：" + errorText(data, "未知错误"), "err");
      return;
    }
    document.getElementById("pair-code").textContent = data.code;
    document.getElementById("pair-link").value = buildPairLink(data, location.hostname);
    const qr = document.getElementById("pair-qr");
    if (qr) {
      if (data.qr) { qr.src = data.qr; qr.hidden = false; }
      else { qr.hidden = true; qr.removeAttribute("src"); }
    }
    const expires = new Date(data.expires_at);
    pairExpiresAt = Number.isNaN(expires.getTime()) ? Date.now() + 300000 : expires.getTime();
    document.getElementById("pair-expiry").textContent = Number.isNaN(expires.getTime())
      ? "" : "截止 " + expires.toLocaleTimeString("zh-CN", {hour12: false});
    document.getElementById("pair-code-card").hidden = false;
    inputsMessage("", "");
    stopPairCountdown();
    tickPairCountdown();
    pairCountdownTimer = setInterval(tickPairCountdown, 1000);
  } catch (error) {
    inputsMessage("生成配对码失败：" + error, "err");
  } finally {
    if (btn) { btn.disabled = false; btn.textContent = "生成配对码"; }
  }
}

function renderPhoneDevices(data) {
  const list = document.getElementById("phone-devices-list");
  if (!list) return;
  list.innerHTML = "";
  if (!data.enabled) {
    const note = document.createElement("div");
    note.className = "note";
    note.textContent = "手机输入未启用。";
    list.appendChild(note);
    return;
  }
  const devices = data.devices || [];
  const connected = new Set((data.connected || []).map(item => item.id));
  if (!devices.length) {
    const note = document.createElement("div");
    note.className = "note";
    note.textContent = "还没有配对手机。点上方「生成配对码」，用手机打开链接或输入 6 位码。";
    list.appendChild(note);
    return;
  }
  for (const device of devices) {
    const row = document.createElement("div");
    row.className = "device-row";
    const main = document.createElement("div");
    main.className = "device-main";
    const name = document.createElement("div");
    name.className = "device-name";
    name.textContent = device.name || device.id;
    if (connected.has(device.id)) name.appendChild(makeBadge("在线", "connected"));
    const meta = document.createElement("div");
    meta.className = "device-meta";
    meta.textContent = [device.id, device.last_seen ? "最后在线 " + fmtAgo(device.last_seen) : "从未连接"].filter(Boolean).join(" · ");
    main.append(name, meta);
    const actions = document.createElement("div");
    actions.className = "device-actions";
    const revoke = document.createElement("button");
    revoke.className = "btn small";
    revoke.textContent = "吊销";
    revoke.addEventListener("click", () => revokePhone(device.id, device.name || device.id));
    actions.appendChild(revoke);
    row.append(main, actions);
    list.appendChild(row);
  }
}

async function loadPhoneDevices() {
  const list = document.getElementById("phone-devices-list");
  if (!list) return;
  try {
    const res = await fetch("/api/phone/devices");
    const data = await res.json();
    renderPhoneDevices(data || {});
  } catch (error) {
    inputsMessage("加载手机设备失败：" + error, "err");
  }
}

async function revokePhone(id, name) {
  if (!confirm("吊销「" + name + "」的配对？该手机需要重新配对才能继续使用。")) return;
  try {
    const res = await fetch("/api/phone/revoke", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({device_id: id}),
    });
    const data = await res.json();
    if (!data.ok) {
      inputsMessage("吊销失败：" + errorText(data, "未知错误"), "err");
      return;
    }
    inputsMessage(data.disconnected ? "已吊销 " + name + "，并断开了它正在进行的会话" : "已吊销 " + name, "ok");
    await loadPhoneDevices();
    await loadInputs();
  } catch (error) {
    inputsMessage("吊销失败：" + error, "err");
  }
}

document.getElementById("inputs-refresh").addEventListener("click", () => { loadInputs(); loadPhoneDevices(); });
document.getElementById("phone-devices-refresh").addEventListener("click", loadPhoneDevices);
document.getElementById("phone-pair-btn").addEventListener("click", () => generatePairCode(false));
document.getElementById("pair-link-copy").addEventListener("click", () => {
  const input = document.getElementById("pair-link");
  if (!input.value || !navigator.clipboard || !navigator.clipboard.writeText) return;
  navigator.clipboard.writeText(input.value)
    .then(() => inputsMessage("配对链接已复制", "ok"))
    .catch(() => {});
});

let historyEntries = [];

function historyMessage(text, cls) {
  const node = document.getElementById("history-msg");
  if (!node) return;
  node.textContent = text || "";
  node.className = "msg " + (cls || "");
}

function renderHistory() {
  const list = document.getElementById("history-list");
  if (!list) return;
  const query = document.getElementById("history-search").value.trim().toLowerCase();
  list.innerHTML = "";
  const items = historyEntries.filter(entry => !query || (entry.text || "").toLowerCase().includes(query) || (entry.raw_text || "").toLowerCase().includes(query));
  if (!items.length) {
    const empty = document.createElement("div");
    empty.className = "note";
    empty.textContent = query ? "没有匹配的历史记录。" : "还没有听写记录。";
    list.appendChild(empty);
    return;
  }
  for (const entry of items) {
    const row = document.createElement("div");
    row.className = "device-row history-row";
    const main = document.createElement("div");
    main.className = "device-main";
    const text = document.createElement("div");
    text.className = "history-text";
    text.textContent = entry.text || "";
    const meta = document.createElement("div");
    meta.className = "history-meta";
    const at = entry.at ? new Date(entry.at).toLocaleString("zh-CN", {hour12: false}) : "";
    meta.textContent = [at, entry.provider, entry.latency_ms ? entry.latency_ms + " ms" : "", entry.processed ? "已润色" : "", entry.truncated ? "已截断" : "", entry.source_name || entry.address].filter(Boolean).join(" · ");
    main.append(text, meta);
    const actions = document.createElement("div");
    actions.className = "history-actions";
    const copy = document.createElement("button");
    copy.className = "btn small";
    copy.textContent = "复制";
    copy.addEventListener("click", () => {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(entry.text || "").then(() => historyMessage("已复制", "ok")).catch(() => {});
      }
    });
    const remove = document.createElement("button");
    remove.className = "btn small";
    remove.textContent = "删除";
    remove.addEventListener("click", () => deleteHistory(entry.id));
    actions.append(copy, remove);
    row.append(main, actions);
    list.appendChild(row);
  }
}

async function loadHistory() {
  try {
    const res = await fetch("/api/history");
    const data = await res.json();
    if (!data.ok) {
      historyMessage(errorText(data, "加载历史失败"), "err");
      return;
    }
    historyEntries = data.entries || [];
    renderHistory();
    syncHistoryActions();
    historyMessage("", "");
  } catch (error) {
    historyMessage("加载历史失败：" + error, "err");
  }
}

async function deleteHistory(id) {
  const res = await fetch("/api/history/delete", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({id}),
  });
  const data = await res.json();
  if (!data.ok) {
    historyMessage("删除失败：" + errorText(data), "err");
    return;
  }
  await loadHistory();
}

function setListening(active, source, name) {
  let label = "等待语音";
  if (active) {
    const who = name || (source ? (source === "phone" ? "手机" : "遥控器") : "");
    label = who ? who + " · 正在聆听…" : "正在聆听…";
  }
  const node = document.getElementById("subtitle-live");
  if (node) {
    node.classList.toggle("listening", !!active);
    const text = node.querySelector("span");
    if (text) text.textContent = label;
  }
  document.body.classList.toggle("listening", !!active);
  if (lastStatus) {
    setChip("chip-mic",
      active ? label : (lastStatus.mic_active ? "语音就绪" : "语音未运行"),
      active ? "warn" : (lastStatus.mic_active ? "good" : "bad"));
  }
  const state = document.getElementById("guided-state");
  if (state) {
    if (active) {
      state.textContent = label;
    } else if (lastStatus) {
      const ready = !lastStatus.asr_error && !!lastStatus.asr_provider;
      state.textContent = ready ? "已就绪" : "待检查";
    } else {
      state.textContent = "已就绪";
    }
  }
}

let activeConfig = "overview";
const TAB_LABELS = { connection: "概览", keys: "按键映射", devices: "设备", subtitle: "字幕", history: "历史", permissions: "权限" };
const CFG_LABELS = { overview: "概览", asr: "语音识别", llm: "文本模型", text: "词典短语" };

function syncNav() {
  const activeTab = document.querySelector(".tab-btn.active");
  const tab = activeTab ? activeTab.dataset.tab : "connection";
  document.querySelectorAll(".side-item").forEach(item => {
    let on = false;
    if (item.dataset.tab) on = item.dataset.tab === tab;
    else if (item.dataset.cfg) on = tab === "connection" && item.dataset.cfg === activeConfig;
    item.classList.toggle("active", on);
    if (on) item.setAttribute("aria-current", "page");
    else item.removeAttribute("aria-current");
  });
  const crumb = document.getElementById("crumb");
  const label = tab === "connection" ? (CFG_LABELS[activeConfig] || "概览") : (TAB_LABELS[tab] || "");
  if (crumb) crumb.textContent = label;
  document.title = "小遥 · " + label;
  const footer = document.getElementById("conn-footer");
  if (footer) footer.hidden = tab !== "permissions";
  const configNav = document.querySelector(".config-nav");
  if (configNav) configNav.style.display = activeConfig === "overview" ? "none" : "";
}

function showConfigPanel(name) {
  activeConfig = name;
  document.querySelectorAll(".config-tab").forEach(button => {
    button.classList.toggle("active", button.dataset.cfg === name);
  });
  document.querySelectorAll(".cfg-panel").forEach(panel => {
    panel.classList.toggle("active", panel.id === "cfg-" + name);
  });
  syncNav();
}

function openConfigPanel(name) {
  goTab("connection");
  showConfigPanel(name);
  if (name === "asr") loadASRConfig();
  if (name === "llm") {
    loadLLMConfig();
    loadLLMAppRules();
    syncLLMEnabledState();
  }
  if (name === "subtitle") {
    loadStreamingToggle();
    loadWakeToggle();
    loadLookbackToggle();
  }
  if (name === "text") loadTextConfig();
}

document.querySelectorAll(".config-tab").forEach(button => {
  button.addEventListener("click", () => {
    showConfigPanel(button.dataset.cfg);
    window.scrollTo(0, 0);
    const mainEl = document.querySelector("main");
    if (mainEl) mainEl.scrollTop = 0;
    if (button.dataset.cfg === "asr") loadASRConfig();
    if (button.dataset.cfg === "llm") loadLLMConfig();
    if (button.dataset.cfg === "text") loadTextConfig();
  });
});

document.querySelectorAll(".side-item").forEach(item => {
  item.addEventListener("click", () => {
    if (item.dataset.cfg) openConfigPanel(item.dataset.cfg);
    else if (item.dataset.tab) goTab(item.dataset.tab);
  });
});

const guidedSubtitle = document.getElementById("guided-subtitle");
if (guidedSubtitle) guidedSubtitle.addEventListener("click", () => goTab("subtitle"));
async function loadStats() {
  const el = document.getElementById("stats-utterances");
  if (!el) return;
  try {
    const res = await fetch("/api/stats");
    const data = await res.json();
    el.textContent = (data.utterances || 0).toLocaleString();
    document.getElementById("stats-characters").textContent = (data.characters || 0).toLocaleString();
    document.getElementById("stats-speech").textContent = fmtDuration(data.speech_ms);
    document.getElementById("stats-longest").textContent = data.longest_ms ? fmtDuration(data.longest_ms) : "—";
    renderStatsDays(data.days || {});
    renderStatsHeatmap(data.days || {});
  } catch (error) {
    /* overview stays usable without stats */
  }
}

function maintenanceMsg(text, kind) {
  const el = document.getElementById("maintenance-msg");
  if (!el) return;
  el.textContent = text;
  el.style.color = kind === "err" ? "var(--red)" : "var(--muted)";
}

function downloadJSON(res, filename) {
  return res.blob().then(blob => {
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = filename;
    a.click();
    URL.revokeObjectURL(a.href);
  });
}

const configExportBtn = document.getElementById("config-export");
if (configExportBtn) configExportBtn.addEventListener("click", async () => {
  try {
    const res = await fetch("/api/config/export");
    if (!res.ok) return maintenanceMsg("导出失败（" + res.status + "）", "err");
    await downloadJSON(res, "xiaoyao-config.json");
    maintenanceMsg("配置已导出。", "ok");
  } catch (error) {
    maintenanceMsg("导出失败：" + error, "err");
  }
});
const configImportBtn = document.getElementById("config-import");
const configImportFile = document.getElementById("config-import-file");
if (configImportBtn && configImportFile) {
  configImportBtn.addEventListener("click", () => configImportFile.click());
  configImportFile.addEventListener("change", async (event) => {
    const file = event.target.files[0];
    event.target.value = "";
    if (!file) return;
    try {
      const res = await fetch("/api/config/import", { method: "POST", headers: { "Content-Type": "application/json" }, body: await file.text() });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) return maintenanceMsg("导入失败：" + (data.error || res.status), "err");
      maintenanceMsg("导入成功，重启服务后生效。", "ok");
      if (needsRestart(data)) showRestartBanner();
    } catch (error) {
      maintenanceMsg("导入失败：" + error, "err");
    }
  });
}
function downloadBlob(bytes, filename, type) {
  const a = document.createElement("a");
  a.href = URL.createObjectURL(new Blob([bytes], { type: type || "application/octet-stream" }));
  a.download = filename;
  a.click();
  URL.revokeObjectURL(a.href);
}

const diagnosticsBtn = document.getElementById("diagnostics-download");
if (diagnosticsBtn) diagnosticsBtn.addEventListener("click", async () => {
  try {
    const passphrase = prompt("设置诊断包加密口令（解密时需要；取消或留空则不导出）");
    if (passphrase === null || passphrase === "") {
      return maintenanceMsg("已取消导出：诊断包需要口令加密。", "err");
    }
    const res = await fetch("/api/diagnostics");
    if (!res.ok) return maintenanceMsg("生成失败（" + res.status + "）", "err");
    const plain = new TextEncoder().encode(JSON.stringify(await res.json(), null, 2));
    const pack = await sealDiagPack(plain, passphrase);
    downloadBlob(pack, "xiaoyao-diagnostics.ovbdiag");
    maintenanceMsg("诊断包已加密下载；解密：xiaoyao diag xiaoyao-diagnostics.ovbdiag -o out.json", "ok");
  } catch (error) {
    maintenanceMsg("生成失败：" + error, "err");
  }
});

// renderStatsHeatmap draws the 26-week calendar (SayAll 182 天热力图).
function renderStatsHeatmap(days) {
  const host = document.getElementById("stats-heat");
  if (!host) return;
  host.innerHTML = "";
  const today = new Date();
  const start = new Date(today);
  start.setDate(today.getDate() - 181);
  // Align the first column to a full week (start on Sunday).
  start.setDate(start.getDate() - start.getDay());
  let peak = 1;
  for (const key in days) {
    const ms = (days[key] && days[key].speech_ms) || 0;
    if (ms > peak) peak = ms;
  }
  let any = false;
  for (let cursor = new Date(start); cursor <= today; cursor.setDate(cursor.getDate() + 1)) {
    const key = cursor.getFullYear() + "-" + String(cursor.getMonth() + 1).padStart(2, "0") + "-" + String(cursor.getDate()).padStart(2, "0");
    const cell = document.createElement("div");
    const stat = days[key];
    const ms = (stat && stat.speech_ms) || 0;
    if (ms > 0) any = true;
    cell.className = "heat-cell";
    if (ms > 0) {
      const level = Math.ceil((ms / peak) * 4);
      cell.classList.add("l" + Math.min(4, Math.max(1, level)));
    }
    cell.title = key + (stat ? "：" + stat.utterances + " 次，" + fmtDuration(stat.speech_ms) : "：无记录");
    host.appendChild(cell);
  }
  const caption = document.getElementById("heat-caption");
  if (caption) {
    caption.textContent = any
      ? "最近 26 周 · 颜色越深表示当天说话越多"
      : "最近 26 周 · 暂无数据，说一句话后这里会亮起来";
  }
}

const mcpCreateBtn = document.getElementById("mcp-create");
if (mcpCreateBtn) mcpCreateBtn.addEventListener("click", async () => {
  const label = (document.getElementById("mcp-label").value || "").trim();
  if (!label) return mcpMsg("请填写客户端名称。", "err");
  try {
    const res = await fetch("/api/mcp/tokens", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ label }) });
    const data = await res.json();
    if (!res.ok) return mcpMsg("创建失败：" + (data.error || res.status), "err");
    const snippet = document.getElementById("mcp-snippet");
    snippet.style.display = "block";
    snippet.textContent = "把下面这段粘进该客户端的 MCP 配置（只显示这一次）：\n" + JSON.stringify(data.snippet, null, 2);
    document.getElementById("mcp-label").value = "";
    mcpMsg("授权已创建，请立刻复制上面的配置。", "ok");
    loadMCPTokens();
  } catch (error) {
    mcpMsg("创建失败：" + error, "err");
  }
});

async function loadMCPTokens() {
  const host = document.getElementById("mcp-token-list");
  if (!host) return;
  try {
    const res = await fetch("/api/mcp/tokens");
    const data = await res.json();
    host.innerHTML = "";
    const tokens = data.tokens || [];
    if (!tokens.length) {
      host.innerHTML = '<div class="sub">还没有授权。创建后，把生成的配置粘进 Claude Code、Codex 等客户端即可只读查询听写历史。</div>';
      return;
    }
    for (const item of tokens) {
      const row = document.createElement("div");
      row.style.cssText = "display:flex;gap:8px;align-items:center;padding:6px 0;border-top:1px solid var(--line);flex-wrap:wrap;";
      const label = document.createElement("span");
      label.textContent = item.label + (item.revoked ? "（已撤销）" : "");
      label.style.cssText = "flex:1;min-width:140px;" + (item.revoked ? "opacity:.5;" : "");
      const used = document.createElement("span");
      used.className = "sub";
      const usedAt = item.last_used ? new Date(item.last_used) : null;
  used.textContent = usedAt && usedAt.getFullYear() > 1970 ? "最近使用 " + usedAt.toLocaleString() : "从未使用";
      row.appendChild(label);
      row.appendChild(used);
      if (!item.revoked) {
        const btn = document.createElement("button");
        btn.className = "btn small";
        btn.textContent = "撤销";
        btn.addEventListener("click", async () => {
          const res = await fetch("/api/mcp/tokens/revoke", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ id: item.id }) });
          if (res.ok) { mcpMsg("已撤销，" + item.label + " 的下一次调用即失败。", "ok"); loadMCPTokens(); }
        });
        row.appendChild(btn);
      }
      host.appendChild(row);
    }
  } catch (error) {
    host.innerHTML = '<div class="sub">加载授权列表失败。</div>';
  }
}

function mcpMsg(text, kind) {
  const el = document.getElementById("mcp-msg");
  if (!el) return;
  el.textContent = text;
  el.style.color = kind === "err" ? "var(--red)" : "var(--muted)";
}
loadMCPTokens();

function renderStatsDays(days) {
  const host = document.getElementById("stats-days");
  if (!host) return;
  host.innerHTML = "";
  const today = new Date();
  const values = [];
  for (let i = 13; i >= 0; i--) {
    const day = new Date(today);
    day.setDate(today.getDate() - i);
    const key = day.getFullYear() + "-" + String(day.getMonth() + 1).padStart(2, "0") + "-" + String(day.getDate()).padStart(2, "0");
    values.push({ key, label: (day.getMonth() + 1) + "/" + day.getDate(), ms: (days[key] && days[key].speech_ms) || 0, count: (days[key] && days[key].utterances) || 0 });
  }
  const peak = Math.max(...values.map(v => v.ms), 1);
  for (const item of values) {
    const col = document.createElement("div");
    col.className = "stats-day";
    const bar = document.createElement("div");
    bar.className = "stats-bar";
    bar.style.height = item.ms ? Math.max(4, Math.round((item.ms / peak) * 56)) + "px" : "2px";
    if (!item.ms) bar.style.opacity = "0.18";
    bar.title = item.label + "：" + item.count + " 次，" + fmtDuration(item.ms);
    const label = document.createElement("div");
    label.className = "stats-day-label";
    label.textContent = item.label;
    col.appendChild(bar);
    col.appendChild(label);
    host.appendChild(col);
  }
}

const guidedSettings = document.getElementById("guided-settings");
if (guidedSettings) guidedSettings.addEventListener("click", () => openConfigPanel("asr"));
const recentAll = document.getElementById("overview-recent-all");
if (recentAll) recentAll.addEventListener("click", () => goTab("history"));

async function loadOverviewRecent() {
  const list = document.getElementById("overview-recent");
  if (!list) return;
  try {
    const res = await fetch("/api/history");
    const data = await res.json();
    const entries = (data.entries || []).slice(0, 3);
    const count = document.getElementById("overview-recent-count");
    const all = document.getElementById("overview-recent-all");
    list.innerHTML = "";
    if (count) {
      count.textContent = entries.length ? "最近 " + entries.length + " 条" : "";
      count.hidden = !entries.length;
    }
    if (all) all.hidden = !entries.length;
    if (!entries.length) {
      const empty = document.createElement("div");
      empty.className = "recent-empty";
      empty.textContent = "还没有听写记录。把光标放进输入框，按住语音键说一句试试。";
      list.appendChild(empty);
      return;
    }
    for (const entry of entries) {
      const row = document.createElement("div");
      row.className = "recent-row";
      const time = document.createElement("time");
      if (entry.at) {
        const at = new Date(entry.at);
        const sameDay = at.toDateString() === new Date().toDateString();
        time.textContent = (sameDay ? "" : (at.getMonth() + 1) + "/" + at.getDate() + " ") + at.toTimeString().slice(0, 5);
      }
      const text = document.createElement("div");
      text.className = "txt";
      text.textContent = entry.text || "";
      row.append(time, text);
      if (entry.latency_ms) {
        const dur = document.createElement("span");
        dur.className = "dur";
        dur.textContent = entry.latency_ms + " ms";
        row.appendChild(dur);
      }
      if (entry.processed) {
        const tag = document.createElement("span");
        tag.className = "tag";
        tag.textContent = "已润色";
        row.appendChild(tag);
      }
      list.appendChild(row);
    }
  } catch (error) {}
}

const initialConfig = new URLSearchParams(location.search).get("cfg");
if (["overview", "asr", "llm", "text"].includes(initialConfig)) {
  showConfigPanel(initialConfig);
}

function connectEvents() {
  const source = new EventSource("/events");
  source.onmessage = (event) => {
    let data;
    try { data = JSON.parse(event.data); } catch (error) { return; }
    if (data.event === "key") {
      highlight(data.key, data.value === 1);
      logKey(data.key, data.value === 1);
      if (data.value === 0) {
        setTimeout(() => highlight(data.key, false), 150);
      }
    } else if (data.event === "listening") {
      setListening(data.active, data.source, data.source_name);
    } else if (data.event === "snapshot") {
      setCaptureUi(!!data.capturing);
      if (data.listening) setListening(!!data.listening.active, data.listening.source, data.listening.source_name);
    } else if (data.event === "stream_gap") {
      // Events were dropped to keep the stream live; resync (I4).
      loadStatus(false);
    } else if (data.event === "transcript") {
      if (data.partial) {
        upsertPartialLine(data.text);
      } else {
        clearPartialLine();
        addSubtitle(data.text, data.at);
        setTimeout(loadOverviewRecent, 500);
        setTimeout(loadStats, 500);
      }
    } else if (data.event === "phrase") {
      // A common phrase is user-authored text, not speech: it shows up live
      // with its own styling and never enters the history panel.
      addSubtitle("〔常用语 " + data.slot + "〕 " + data.text, data.at, {kind: "phrase"});
    } else if (data.event === "phrase_picker") {
      openPhrasePicker();
    } else if (data.event === "command") {
      // A voice command is a spoken act: it runs a key combo and, like a
      // phrase, never enters history.
      addSubtitle("〔指令〕" + data.text + " → " + data.combo, data.at, {kind: "phrase"});
    } else if (data.event === "capture_started") {
      setCaptureUi(true);
      setChip("chip-keyd", "捕获中 · 注入已暂停", "warn");
    } else if (data.event === "capture_stopped" || data.event === "capture_closed") {
      setCaptureUi(false);
      loadStatus(false);
      if (dirty) scheduleSave();
    }
  };
  source.onerror = () => {
    setListening(false);
    setTimeout(connectEvents, 3000);
  };
}

document.querySelectorAll(".tab-btn").forEach(button => {
  button.addEventListener("click", () => {
    document.querySelectorAll(".tab-btn").forEach(item => item.classList.remove("active"));
    document.querySelectorAll(".tab").forEach(item => item.classList.remove("active"));
    button.classList.add("active");
    document.getElementById("tab-" + button.dataset.tab).classList.add("active");
    window.scrollTo(0, 0);
    const mainEl = document.querySelector("main");
    if (mainEl) mainEl.scrollTop = 0;
    if (button.dataset.tab !== "devices") stopPairCountdown();
    if (button.dataset.tab === "connection") {
      showConfigPanel("overview");
      loadStatus(false);
      loadStats();
      loadASRConfig();
      loadLLMConfig();
      loadTextConfig();
    } else if (button.dataset.tab === "devices") {
      loadDevices();
      loadDiscovery();
      loadInputs();
      loadPhoneDevices();
    } else if (button.dataset.tab === "history") {
      loadHistory();
    } else if (button.dataset.tab === "subtitle") {
      const active = subtitleLinesEl?.querySelector(".subtitle-line.active");
      if (active) active.scrollIntoView({block: "center"});
    }
    syncNav();
  });
});

const initialTab = new URLSearchParams(location.search).get("tab");
if (["connection", "keys", "devices", "subtitle", "history", "permissions"].includes(initialTab)) {
  const initialButton = document.querySelector(`.tab-btn[data-tab="${initialTab}"]`);
  if (initialButton) initialButton.click();
}
if (initialConfig && ["overview", "asr", "llm", "text"].includes(initialConfig)) {
  showConfigPanel(initialConfig);
}

document.querySelectorAll("#remote [data-key]").forEach(button => {
  button.addEventListener("click", () => selectKey(button.dataset.key, true));
});

const REMOTE_FIGURE_KEY = "remote-figure";
let remoteFigurePinned = false;

function setRemoteFigure(model, pinned) {
  document.querySelectorAll("#remote .remote-photo").forEach(figure => {
    figure.hidden = figure.dataset.model !== model;
  });
  document.querySelectorAll(".figure-btn").forEach(button => {
    button.classList.toggle("active", button.dataset.model === model);
  });
  if (pinned) {
    remoteFigurePinned = true;
    try { localStorage.setItem(REMOTE_FIGURE_KEY, model); } catch (error) {}
  }
}

document.querySelectorAll(".figure-btn").forEach(button => {
  button.addEventListener("click", () => setRemoteFigure(button.dataset.model, true));
});

// Follow the active device until the user picks a figure explicitly.
function syncRemoteFigureToActive() {
  if (remoteFigurePinned) return;
  const active = deviceItems.find(item => item.active) || deviceItems[0];
  const label = ((active && (active.name + " " + active.id)) || "").toLowerCase();
  const model = label.includes("arn9") || label.includes("小米蓝牙语音遥控器") ? "arn9" : "rc003";
  setRemoteFigure(model, false);
}

(function initRemoteFigure() {
  let saved = null;
  try { saved = localStorage.getItem(REMOTE_FIGURE_KEY); } catch (error) {}
  if (saved === "rc003" || saved === "arn9") {
    remoteFigurePinned = true;
    setRemoteFigure(saved, false);
    return;
  }
  setRemoteFigure("rc003", false);
})();

document.getElementById("enabled").addEventListener("change", (event) => {
  setEnabledState(event.target.checked);
  if (event.target.checked) {
    const stored = localStorage.getItem("xiaomiMapping");
    if (stored) {
      try {
        const parsed = JSON.parse(stored);
        for (const key in parsed) {
          if (key in DEFAULTS && selects[key]) {
            selects[key].value = parsed[key];
            mapping[key] = parsed[key];
          }
        }
      } catch (error) {}
    }
  }
  dirty = true;
  scheduleSave();
});

document.getElementById("capture-btn").addEventListener("click", toggleCapture);
document.getElementById("reset").addEventListener("click", resetDefaults);

// loadPermissions renders the live status pills on the permissions page
// (design-qa P2 授权状态展示); the probes run on the agent side on demand.
const PERM_LABELS = { ok: "已就绪", todo: "待处理", na: "不适用", unknown: "未知" };

function renderPermissions(items) {
  for (const item of items || []) {
    for (const li of document.querySelectorAll('#tab-permissions li[data-perm="' + item.id + '"]')) {
      let badge = li.querySelector(".perm-status");
      if (!badge) {
        badge = document.createElement("span");
        badge.className = "perm-status";
        li.prepend(badge);
      }
      badge.textContent = PERM_LABELS[item.status] || item.status;
      badge.className = "perm-status " + item.status;
      badge.title = item.detail + (item.fix ? "；修复：" + item.fix : "");
    }
  }
}

async function loadPermissions() {
  try {
    const res = await fetch("/api/permissions");
    if (!res.ok) return;
    renderPermissions((await res.json()).items);
  } catch (error) {}
}

document.getElementById("btn-refresh").addEventListener("click", async () => {
  await loadStatus(false);
  await loadPermissions();
  document.getElementById("conn-msg").textContent = "状态已刷新。";
  document.getElementById("conn-msg").className = "msg ok";
});

document.getElementById("btn-reconnect").addEventListener("click", async () => {
  const msg = document.getElementById("conn-msg");
  msg.textContent = "正在连接遥控器…";
  msg.className = "msg";
  const res = await fetch("/api/reconnect", {method: "POST", headers: {"Content-Type": "application/json"}, body: "{}"});
  const data = await res.json();
  msg.textContent = data.ok ? "连接成功。" : ("连接失败：" + errorText(data).slice(0, 160));
  msg.className = "msg " + (data.ok ? "ok" : "err");
  loadStatus(false);
});

document.getElementById("goto-devices")?.addEventListener("click", () => goTab("devices"));
document.getElementById("btn-onboarding")?.addEventListener("click", openWelcome);

document.getElementById("btn-restart-mic").addEventListener("click", async () => {
  const msg = document.getElementById("conn-msg");
  msg.textContent = "正在重启麦克风桥接…";
  msg.className = "msg";
  const res = await fetch("/api/restart-mic", {method: "POST", headers: {"Content-Type": "application/json"}, body: "{}"});
  const data = await res.json();
  msg.textContent = data.ok ? "麦克风桥接已重启。" : ("重启失败：" + errorText(data));
  msg.className = "msg " + (data.ok ? "ok" : "err");
  setTimeout(() => loadStatus(false), 800);
});

const asrSelect = document.getElementById("asr-select");
if (asrSelect) {
  asrSelect.addEventListener("change", async () => {
    const msg = document.getElementById("conn-msg");
    msg.textContent = "正在切换语音识别…";
    msg.className = "msg";
    const res = await fetch("/api/asr/switch", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({provider: asrSelect.value}),
    });
    const data = await res.json();
    msg.textContent = data.ok ? ("已切换到 " + asrSelect.value) : ("切换失败：" + errorText(data));
    msg.className = "msg " + (data.ok ? "ok" : "err");
    setTimeout(() => loadStatus(false), 600);
  });
}

// ---- keymap profiles ----
const keymapProfileSelect = document.getElementById("keymap-profile");
if (keymapProfileSelect) {
  keymapProfileSelect.addEventListener("change", async () => {
    const status = document.getElementById("save-status");
    status.textContent = "正在应用预设…";
    status.className = "save-status";
    const res = await fetch("/api/keymap/apply", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({profile: keymapProfileSelect.value}),
    });
    const data = await res.json();
    if (data.ok) {
      status.textContent = "已应用预设 " + keymapProfileSelect.value;
      status.className = "save-status ok";
      await loadStatus(false, true);
    } else {
      status.textContent = "应用失败：" + errorText(data);
      status.className = "save-status err";
    }
  });
}

document.getElementById("profile-save").addEventListener("click", async () => {
  const status = document.getElementById("save-status");
  const name = prompt("预设名称（小写字母、数字、_ 或 -）", "my-profile");
  if (!name) return;
  const target = name.trim();
  if ([...keymapProfileSelect.options].some(option => option.value === target)) {
    if (!confirm("预设 " + target + " 已存在，覆盖它？")) return;
  }
  status.textContent = "正在保存预设…";
  status.className = "save-status";
  try {
    const res = await fetch("/api/keymap/save", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({name: name.trim(), mapping: collectMappingValues()}),
    });
    const data = await res.json();
    if (!data.ok) {
      status.textContent = "保存预设失败：" + errorText(data);
      status.className = "save-status err";
      return;
    }
    status.textContent = "已保存并应用预设 " + name.trim();
    status.className = "save-status ok";
    await loadStatus(false, true);
  } catch (error) {
    status.textContent = "保存预设失败：" + error;
    status.className = "save-status err";
  }
});

document.getElementById("profile-delete").addEventListener("click", async () => {
  const status = document.getElementById("save-status");
  const name = keymapProfileSelect.value;
  if (!name) return;
  if (!confirm("删除预设 " + name + "？")) return;
  const res = await fetch("/api/keymap/delete", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({name}),
  });
  const data = await res.json();
  if (!data.ok) {
    status.textContent = "删除预设失败：" + errorText(data);
    status.className = "save-status err";
    return;
  }
  status.textContent = "已删除预设 " + name;
  status.className = "save-status ok";
  await loadStatus(false);
});

// ---- ASR presets ----
const ASR_PRESETS = [
  {id: "mimo", label: "MiMo 云端", type: "mimo", model: "mimo-v2.5-asr", base: "", language: "auto"},
  {id: "openai", label: "OpenAI Whisper", type: "openai", model: "whisper-1", base: "https://api.openai.com/v1", language: "auto"},
  {id: "openai-compatible", label: "OpenAI 兼容 ASR", type: "openai", model: "whisper-1", base: "http://127.0.0.1:8080/v1", language: "auto"},
  {id: "whispercpp", label: "本地 whisper.cpp", type: "whispercpp", model: "", base: "", binary: "whisper-cli", modelPath: "~/whisper.cpp/models/ggml-base.bin", language: "auto"},
  {id: "command", label: "本地命令 ASR", type: "command", model: "", base: "", binary: "sherpa-onnx-offline", language: "auto"},
];

function populateASRPresets() {
  const select = document.getElementById("asr-preset");
  if (!select) return;
  select.innerHTML = '<option value="">选择快速模板…</option>';
  for (const preset of ASR_PRESETS) {
    const option = document.createElement("option");
    option.value = preset.id;
    option.textContent = preset.label;
    select.appendChild(option);
  }
}

function applyASRPreset(id) {
  const preset = ASR_PRESETS.find(item => item.id === id);
  if (!preset || !asrConfigData) return;
  const providers = asrConfigData.providers || (asrConfigData.providers = {});
  const exists = !!providers[preset.id];
  if (!exists) {
    providers[preset.id] = {
      type: preset.type,
      model: preset.model || "",
      base_url: preset.base || "",
      binary: preset.binary || "",
      model_path: preset.modelPath || "",
      language: preset.language || "auto",
    };
    unsavedASR.add(preset.id);
  }
  asrConfigData.provider = preset.id;
  populateModelProviders();
  modelProvider.value = preset.id;
  fillModelFields(preset.id);
  syncModelFields();
  modelMessage(exists
    ? "已切换到配置「" + preset.id + "」，按需修改后保存"
    : "已按模板新建配置「" + preset.id + "」，填好 Key 或路径后保存并启用", "ok");
}

// ---- glance console: hero verdict + attention cards ----
let lastStatus = null;
let lastInputs = null;

function goTab(name) {
  const button = document.querySelector(`.tab-btn[data-tab="${name}"]`);
  if (button) button.click();
}

// OPS-06: readiness answers "can dictation work" — the ASR chain is the core,
// while the phone, the remote, the remapper and the virtual mic are optional
// gaps that must not hold the verdict hostage.
function renderAttention(status) {
  const phone = document.getElementById("attn-phone-status");
  if (phone) {
    const data = lastInputs || {};
    const running = !!data.enabled;
    const configured = !!data.configured;
    const paired = (data.sources || []).filter(item => item.kind === "phone").length;
    if (paired) {
      phone.textContent = "已配对 " + paired + " 台 · 运行中";
      phone.className = "value good";
    } else if (configured && !running) {
      phone.textContent = "已启用 · 重启后生效";
      phone.className = "value warn";
    } else {
      phone.textContent = "未配对";
      phone.className = "value";
    }
  }
  if (document.getElementById("welcome")?.classList.contains("open")) renderWelcome();
  const asrState = document.getElementById("attn-asr-state");
  if (asrState) {
    const asrOK = status && !status.asr_error;
    asrState.textContent = asrOK ? "运行中" : "配置不可用";
    asrState.className = "attn-state " + (asrOK ? "ok" : "bad");
  }
}

async function loadInputsOverview() {
  try {
    const res = await fetch("/api/inputs");
    lastInputs = await res.json();
  } catch (error) {}
  renderAttention(lastStatus || {});
}

document.querySelectorAll(".attn-card").forEach(card => {
  card.addEventListener("click", () => {
    if (card.dataset.go) goTab(card.dataset.go);
    else if (card.dataset.cfg) openConfigPanel(card.dataset.cfg);
  });
});

// ---- dictionary and snippets ----
let textConfigData = null;

function textMessage(text, cls) {
  const node = document.getElementById("text-config-msg");
  if (!node) return;
  node.textContent = text || "";
  node.className = "msg " + (cls || "");
}

function dictionaryRow(from = "", to = "") {
  const row = document.createElement("div");
  row.className = "rule-row";
  const source = document.createElement("input");
  source.className = "rule-from";
  source.placeholder = "识别错误";
  source.value = from;
  const target = document.createElement("input");
  target.className = "rule-to";
  target.placeholder = "正确写法";
  target.value = to;
  const remove = document.createElement("button");
  remove.className = "btn small";
  remove.textContent = "删除";
  remove.addEventListener("click", () => row.remove());
  row.append(source, target, remove);
  return row;
}

function snippetRow(trigger = "", text = "", slot = 0) {
  const row = document.createElement("div");
  row.className = "rule-row snippet-row";
  const source = document.createElement("input");
  source.className = "rule-from";
  source.placeholder = "触发词，如：收货地址";
  source.value = trigger;
  const target = document.createElement("textarea");
  target.className = "rule-to";
  target.rows = 2;
  target.placeholder = "插入的模板文本";
  target.value = text;
  const remove = document.createElement("button");
  remove.className = "btn small";
  remove.textContent = "删除";
  remove.addEventListener("click", () => row.remove());
  const badge = document.createElement("span");
  badge.className = "rule-slot";
  if (slot > 0 && slot <= 8) {
    badge.textContent = "常用语 " + slot;
    badge.title = "按键映射里选「常用语 " + slot + "（按下插入）」，按一下就上屏这段文字";
  }
  row.append(badge, source, target, remove);
  return row;
}

function renderTextConfig(cfg) {
  const dictionary = document.getElementById("dictionary-list");
  const snippets = document.getElementById("snippet-list");
  if (!dictionary || !snippets) return;
  dictionary.innerHTML = "";
  snippets.innerHTML = "";
  for (const item of cfg.dictionary || []) dictionary.appendChild(dictionaryRow(item.from, item.to));
  (cfg.snippets || []).forEach((item, index) => {
    snippets.appendChild(snippetRow(item.trigger, item.text, index + 1));
  });
  if (!dictionary.children.length) dictionary.appendChild(dictionaryRow());
  if (!snippets.children.length) snippets.appendChild(snippetRow());
  const commands = document.getElementById("commands-list");
  const commandsToggle = document.getElementById("commands-enabled");
  if (commands) {
    commands.innerHTML = "";
    for (const [spoken, combo] of Object.entries(cfg.commands || {})) {
      commands.appendChild(commandRow(spoken, combo));
    }
    if (!commands.children.length) commands.appendChild(commandRow());
  }
  if (commandsToggle) commandsToggle.checked = !!cfg.commands_enabled;
}

async function loadTextConfig() {
  try {
    const res = await fetch("/api/text/config");
    const data = await res.json();
    if (!data.ok) {
      textMessage(errorText(data, "加载词典失败"), "err");
      return;
    }
    textConfigData = data;
    const cfg = data.config || {};
    try {
      const commandsRes = await fetch("/api/text/commands");
      const commandsData = await commandsRes.json();
      if (commandsData.ok) {
        cfg.commands_enabled = commandsData.enabled;
        cfg.commands = commandsData.commands;
      }
    } catch (error) {}
    renderTextConfig(cfg);
    textMessage("", "");
  } catch (error) {
    textMessage("加载词典失败：" + error, "err");
  }
}

// ---- live partial line (S6 边说边滚字) ----
function upsertPartialLine(text) {
  if (!subtitleLinesEl || !text) return;
  let line = document.getElementById("subtitle-partial");
  if (!line) {
    line = document.createElement("div");
    line.id = "subtitle-partial";
    line.className = "subtitle-line partial";
    subtitleLinesEl.appendChild(line);
    if (subtitleEmpty) subtitleEmpty.style.display = "none";
    syncSubtitleActions();
  }
  line.textContent = text;
  line.scrollIntoView({block: "nearest"});
}

function clearPartialLine() {
  document.getElementById("subtitle-partial")?.remove();
}

async function loadStreamingToggle() {
  const toggle = document.getElementById("streaming-toggle");
  if (!toggle) return;
  try {
    const res = await fetch("/api/streaming");
    const data = await res.json();
    if (data.ok) toggle.checked = !!data.enabled;
  } catch (error) {}
  if (toggle.dataset.bound) return;
  toggle.dataset.bound = "1";
  toggle.addEventListener("change", async () => {
    try {
      const res = await fetch("/api/streaming", {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({enabled: toggle.checked}),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || !data.ok) {
        subtitleMsg("边说边滚字保存失败：" + errorText(data, String(res.status)), "err");
        return;
      }
      subtitleMsg("边说边滚字已" + (toggle.checked ? "开启" : "关闭"), "ok");
      if (needsRestart(data)) showRestartBanner();
    } catch (error) {
      subtitleMsg("边说边滚字保存失败：" + error, "err");
    }
  });
}

async function loadWakeToggle() {
  const toggle = document.getElementById("wake-toggle");
  const word = document.getElementById("wake-word");
  if (!toggle) return;
  try {
    const res = await fetch("/api/wake");
    const data = await res.json();
    if (data.ok) {
      toggle.checked = !!data.enabled;
      if (word && data.word) word.value = data.word;
    }
  } catch (error) {}
  if (toggle.dataset.bound) return;
  toggle.dataset.bound = "1";
  const save = async () => {
    try {
      const res = await fetch("/api/wake", {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({enabled: toggle.checked, word: word ? word.value.trim() : ""}),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || !data.ok) {
        subtitleMsg("唤醒词保存失败：" + errorText(data, String(res.status)), "err");
        return;
      }
      subtitleMsg("唤醒词设置已保存", "ok");
      if (needsRestart(data)) showRestartBanner();
    } catch (error) {
      subtitleMsg("唤醒词保存失败：" + error, "err");
    }
  };
  toggle.addEventListener("change", save);
  if (word) word.addEventListener("change", save);
}

async function loadPhonePowerToggle() {
  const toggle = document.getElementById("phone-power-toggle");
  if (!toggle) return;
  try {
    const res = await fetch("/api/phone/power");
    const data = await res.json();
    if (data.ok) toggle.checked = !!data.allow;
  } catch (error) {}
  if (toggle.dataset.bound) return;
  toggle.dataset.bound = "1";
  toggle.addEventListener("change", async () => {
    try {
      const res = await fetch("/api/phone/power", {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({allow: toggle.checked}),
      });
      const data = await res.json();
      inputsMessage(data.ok ? "已保存，重启服务后生效。" : ("保存失败：" + errorText(data)), data.ok ? "ok" : "err");
      if (data.ok && needsRestart(data)) showRestartBanner();
    } catch (error) {
      inputsMessage("保存失败：" + error, "err");
    }
  });
}

async function loadLookbackToggle() {
  const toggle = document.getElementById("lookback-toggle");
  const seconds = document.getElementById("lookback-seconds");
  const recover = document.getElementById("lookback-recover");
  if (!toggle) return;
  try {
    const res = await fetch("/api/lookback");
    const data = await res.json();
    if (data.ok) {
      toggle.checked = !!data.enabled;
      if (seconds && data.seconds) seconds.value = data.seconds;
    }
  } catch (error) {}
  if (toggle.dataset.bound) return;
  toggle.dataset.bound = "1";
  const save = async () => {
    try {
      const res = await fetch("/api/lookback", {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({enabled: toggle.checked, seconds: seconds ? Number(seconds.value) || 30 : 30}),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || !data.ok) {
        subtitleMsg("回眸保存失败：" + errorText(data, String(res.status)), "err");
        return;
      }
      subtitleMsg("回眸设置已保存", "ok");
      if (needsRestart(data)) showRestartBanner();
    } catch (error) {
      subtitleMsg("回眸保存失败：" + error, "err");
    }
  };
  toggle.addEventListener("change", save);
  if (seconds) seconds.addEventListener("change", save);
  if (recover) recover.addEventListener("click", async () => {
    recover.disabled = true;
    try {
      const res = await fetch("/api/lookback/recover", {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({seconds: seconds ? Number(seconds.value) || 30 : 30}),
      });
      const data = await res.json();
      recover.textContent = data.ok ? "识别中…" : (data.error || "无可识别内容");
    } catch (error) {
      recover.textContent = "识别失败";
    }
    setTimeout(() => { recover.textContent = "识别刚才"; recover.disabled = false; }, 1500);
  });
}

// ---- phrase picker overlay (S2) ----
function phraseTextsForPicker() {
  return (textConfigData?.config?.snippets || []).map(item => item.text || "");
}

function renderPhrasePicker() {
  const list = document.getElementById("phrase-picker-list");
  if (!list) return;
  list.innerHTML = "";
  const texts = phraseTextsForPicker();
  for (let slot = 1; slot <= 8; slot++) {
    const text = texts[slot - 1] || "";
    const item = document.createElement("button");
    item.className = "phrase-item";
    const num = document.createElement("span");
    num.className = "num";
    num.textContent = String(slot);
    const body = document.createElement("span");
    body.className = "txt" + (text ? "" : " empty");
    body.textContent = text || "（空槽位）";
    item.append(num, body);
    item.addEventListener("click", () => pickPhrase(slot));
    list.appendChild(item);
  }
}

function openPhrasePicker() {
  const picker = document.getElementById("phrase-picker");
  if (!picker) return;
  renderPhrasePicker();
  const msg = document.getElementById("phrase-picker-msg");
  if (msg) msg.textContent = "";
  picker.classList.add("open");
}

function closePhrasePicker() {
  document.getElementById("phrase-picker")?.classList.remove("open");
}

async function pickPhrase(slot) {
  const msg = document.getElementById("phrase-picker-msg");
  try {
    const res = await fetch("/api/phrases/paste", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({slot}),
    });
    const data = await res.json();
    if (!data.ok) {
      if (msg) msg.textContent = errorText(data, "插入失败");
      return;
    }
    if (msg) msg.textContent = "已插入：" + data.text;
    setTimeout(closePhrasePicker, 600);
  } catch (error) {
    if (msg) msg.textContent = "插入失败：" + error;
  }
}

document.getElementById("phrase-fab")?.addEventListener("click", openPhrasePicker);
document.getElementById("phrase-picker-close")?.addEventListener("click", closePhrasePicker);
document.getElementById("phrase-picker")?.addEventListener("click", (event) => {
  if (event.target.id === "phrase-picker") closePhrasePicker();
});
document.addEventListener("keydown", (event) => {
  const picker = document.getElementById("phrase-picker");
  if (!picker?.classList.contains("open")) return;
  if (event.key === "Escape") {
    closePhrasePicker();
  } else if (event.key >= "1" && event.key <= "8") {
    pickPhrase(Number(event.key));
  }
});

// ---- first-run onboarding (S4) ----
function welcomeSteps(status) {
  status = status || {};
  const phoneLive = !!lastInputs?.enabled && (lastInputs.sources || []).some(item => item.kind === "phone");
  return [
    {
      title: "连接遥控器",
      hint: "系统蓝牙配对后，到「设备」页点扫描已连接设备",
      done: !!(status.ble_connected || status.remote_present),
      go: () => goTab("devices"),
    },
    {
      title: "配置语音识别",
      hint: "MiMo、OpenAI 兼容或本地 whisper.cpp，填好密钥或路径后保存并启用",
      done: !status.asr_error && !!status.asr_provider,
      go: () => openConfigPanel("asr"),
    },
    {
      title: "配对手机（可选）",
      hint: "手机浏览器当无线遥控与外置麦克风，同一局域网即可",
      done: phoneLive,
      go: () => goTab("devices"),
    },
    {
      title: "开始听写",
      hint: "把光标放进任意输入框，按住语音键说话；字幕页可看实时歌词",
      done: false,
      go: () => goTab("subtitle"),
    },
  ];
}

function renderWelcome() {
  const host = document.getElementById("welcome-steps");
  if (!host) return;
  host.innerHTML = "";
  welcomeSteps(lastStatus).forEach((step, index) => {
    const row = document.createElement("div");
    row.className = "welcome-step" + (step.done ? " done" : "");
    const num = document.createElement("span");
    num.className = "num";
    num.textContent = step.done ? "✓" : String(index + 1);
    const body = document.createElement("div");
    body.className = "body";
    const title = document.createElement("div");
    title.className = "title";
    title.textContent = step.title;
    const hint = document.createElement("div");
    hint.className = "hint";
    hint.textContent = step.hint;
    body.append(title, hint);
    const go = document.createElement("button");
    go.className = "btn small";
    go.textContent = step.done ? "查看" : "去配置";
    go.addEventListener("click", () => {
      closeWelcome();
      step.go();
    });
    row.append(num, body, go);
    host.appendChild(row);
  });
}

function openWelcome() {
  renderWelcome();
  document.getElementById("welcome")?.classList.add("open");
}

function closeWelcome(saveFlag) {
  document.getElementById("welcome")?.classList.remove("open");
  if (saveFlag) {
    try { localStorage.setItem("xiaomiOnboarded", "1"); } catch (error) {}
  }
}

function maybeOpenWelcome() {
  const force = new URLSearchParams(location.search).get("onboarding") === "1";
  let seen = false;
  try { seen = localStorage.getItem("xiaomiOnboarded") === "1"; } catch (error) {}
  if (force || !seen) openWelcome();
}

document.getElementById("welcome-start")?.addEventListener("click", () => closeWelcome(true));
document.getElementById("welcome-skip")?.addEventListener("click", () => closeWelcome(true));
document.getElementById("welcome")?.addEventListener("click", (event) => {
  if (event.target.id === "welcome") closeWelcome(true);
});

const DEFAULT_COMMANDS = {
  "复制": "ctrl+c", "粘贴": "ctrl+v", "撤销": "ctrl+z", "全选": "ctrl+a",
  "保存": "ctrl+s", "回车": "enter", "退格": "backspace", "刷新": "f5",
};

const REWRITE_MODES = [
  ["", "跟随全局模式"],
  ["off", "不改写（保持原话）"],
  ["cleanup", "润色（去口头语 + 标点）"],
  ["format", "整理段落"],
  ["translate", "翻译成英文"],
];

function appRuleRow(ruleClass = "", mode = "") {
  const row = document.createElement("div");
  row.className = "rule-row";
  const source = document.createElement("input");
  source.className = "rule-from";
  source.placeholder = "应用类名，如：mail / code / wechat";
  source.value = ruleClass;
  const select = document.createElement("select");
  select.className = "rule-to";
  for (const [value, label] of REWRITE_MODES) {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = label;
    if (value === mode) option.selected = true;
    select.appendChild(option);
  }
  const remove = document.createElement("button");
  remove.className = "btn small";
  remove.textContent = "删除";
  remove.addEventListener("click", () => row.remove());
  row.append(source, select, remove);
  return row;
}

function renderAppRules(rules) {
  const list = document.getElementById("app-rules-list");
  if (!list) return;
  list.innerHTML = "";
  for (const rule of rules || []) list.appendChild(appRuleRow(rule.class, rule.mode));
  if (!list.children.length) list.appendChild(appRuleRow());
}

async function loadLLMAppRules() {
  try {
    const res = await fetch("/api/llm/app-rules");
    const data = await res.json();
    if (data.ok) renderAppRules(data.rules || []);
  } catch (error) {}
}

async function saveLLMAppRules() {
  const msg = document.getElementById("app-rules-msg");
  const rules = [];
  for (const row of document.querySelectorAll("#app-rules-list .rule-row")) {
    const ruleClass = row.querySelector(".rule-from").value.trim();
    const mode = row.querySelector("select").value;
    if (ruleClass) rules.push({class: ruleClass, mode});
  }
  try {
    const res = await fetch("/api/llm/app-rules", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({rules}),
    });
    const data = await res.json();
    if (!data.ok) {
      if (msg) msg.textContent = "保存失败：" + errorText(data);
      return;
    }
    if (msg) msg.textContent = "应用规则已保存并生效";
    loadLLMAppRules();
  } catch (error) {
    if (msg) msg.textContent = "保存失败：" + error;
  }
}

function bindDraftRow(row, fields) {
  // A blank draft row stays clean: the delete button only appears once the
  // row has real content (UX 验收 B#2).
  const remove = row.querySelector("button");
  if (!remove) return row;
  const sync = () => {
    remove.hidden = !fields.some(field => field.value.trim());
  };
  for (const field of fields) field.addEventListener("input", sync);
  sync();
  return row;
}

function commandRow(from = "", to = "") {
  const row = document.createElement("div");
  row.className = "rule-row";
  const source = document.createElement("input");
  source.className = "rule-from";
  source.placeholder = "命令词，如：复制";
  source.value = from;
  const target = document.createElement("input");
  target.className = "rule-to";
  target.placeholder = "快捷键，如：ctrl+c / cmd+c";
  target.value = to;
  const remove = document.createElement("button");
  remove.className = "btn small";
  remove.textContent = "删除";
  remove.addEventListener("click", () => row.remove());
  row.append(source, target, remove);
  return row;
}

function collectTextConfig() {
  const dictionary = [];
  for (const row of document.querySelectorAll("#dictionary-list .rule-row")) {
    const from = row.querySelector(".rule-from").value.trim();
    const to = row.querySelector(".rule-to").value;
    if (from) dictionary.push({from, to});
  }
  const snippets = [];
  for (const row of document.querySelectorAll("#snippet-list .rule-row")) {
    const trigger = row.querySelector(".rule-from").value.trim();
    const text = row.querySelector(".rule-to").value;
    if (trigger) snippets.push({trigger, text});
  }
  const commands = {};
  for (const row of document.querySelectorAll("#commands-list .rule-row")) {
    const spoken = row.querySelector(".rule-from").value.trim();
    const combo = row.querySelector(".rule-to").value.trim();
    if (spoken && combo) commands[spoken] = combo;
  }
  const commandsEnabled = !!document.getElementById("commands-enabled")?.checked;
  return {dictionary, snippets, commands_enabled: commandsEnabled, commands};
}

async function saveTextConfig() {
  textMessage("正在保存…", "");
  const payload = collectTextConfig();
  try {
    const res = await fetch("/api/text/config", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(payload),
    });
    const data = await res.json();
    if (!data.ok) {
      textMessage("保存失败：" + errorText(data), "err");
      return;
    }
    const commandsRes = await fetch("/api/text/commands", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({enabled: payload.commands_enabled, commands: payload.commands}),
    });
    const commandsData = await commandsRes.json();
    if (!commandsData.ok) {
      textMessage("命令保存失败：" + errorText(commandsData), "err");
      return;
    }
    textMessage("词典、短语与语音命令已保存并生效", "ok");
    await loadTextConfig();
  } catch (error) {
    textMessage("保存失败：" + error, "err");
  }
}

// ---- LLM post-processing config ----
let llmConfigData = null;

function llmMessage(text, cls) {
  const node = document.getElementById("llm-config-msg");
  if (!node) return;
  node.textContent = text || "";
  node.className = "msg " + (cls || "");
}

function llmPreset(id) {
  return (llmConfigData?.presets || []).find(item => item.id === id);
}

function populateLLMProviders(selected) {
  const select = document.getElementById("llm-provider");
  if (!select || !llmConfigData) return;
  select.innerHTML = "";
  for (const preset of llmConfigData.presets || []) {
    const option = document.createElement("option");
    option.value = preset.id;
    option.textContent = preset.name;
    select.appendChild(option);
  }
  if (selected) select.value = selected;
}

function fillLLMFields(cfg) {
  if (!cfg) return;
  document.getElementById("llm-enabled").checked = !!cfg.enabled;
  populateLLMProviders(cfg.provider);
  document.getElementById("llm-base-url").value = cfg.base_url || "";
  document.getElementById("llm-model").value = cfg.model || "";
  document.getElementById("llm-api-key-file").value = cfg.api_key_file || "";
  document.getElementById("llm-mode").value = cfg.mode || "cleanup";
  document.getElementById("llm-prompt").value = cfg.prompt || "";
  document.getElementById("llm-timeout").value = cfg.timeout_seconds || 20;
  const preset = llmPreset(cfg.provider);
  if (preset) {
    document.getElementById("llm-model").placeholder = preset.model_hint || "";
    document.getElementById("llm-api-key").placeholder = preset.key_hint || "sk-...";
  }
}

async function loadLLMConfig() {
  try {
    const res = await fetch("/api/llm/config");
    const data = await res.json();
    if (!data.ok) {
      llmMessage(errorText(data, "加载大模型配置失败"), "err");
      return;
    }
    llmConfigData = data;
    fillLLMFields(data.config || {});
    llmMessage("", "");
    if (lastStatus) renderAttention(lastStatus);
  } catch (error) {
    llmMessage("加载大模型配置失败：" + error, "err");
  }
}

// ---- ASR model config ----
let asrConfigData = null;
const unsavedASR = new Set();
const modelProvider = document.getElementById("model-provider");
const modelFields = {
  type: document.getElementById("model-type"),
  model: document.getElementById("model-model"),
  base_url: document.getElementById("model-base-url"),
  api_key_file: document.getElementById("model-api-key-file"),
  binary: document.getElementById("model-binary"),
  model_path: document.getElementById("model-model-path"),
  threads: document.getElementById("model-threads"),
  timeout_seconds: document.getElementById("model-timeout"),
  language: document.getElementById("model-language"),
  args: document.getElementById("model-args"),
};

const modelFieldMap = {
  mimo: ["type", "model", "base_url", "api_key_file", "timeout_seconds", "language", "fallback"],
  openai: ["type", "model", "base_url", "api_key_file", "timeout_seconds", "language", "fallback"],
  whispercpp: ["type", "binary", "model_path", "threads", "timeout_seconds", "language", "fallback"],
  command: ["type", "binary", "model_path", "args", "timeout_seconds", "language", "fallback"],
};

const TYPE_LABELS = {mimo: "MiMo", openai: "OpenAI 兼容", whispercpp: "whisper.cpp 本地", command: "本地命令"};

function syncModelFields() {
  const type = modelFields.type.value;
  const visible = new Set(modelFieldMap[type] || Object.keys(modelFields).concat(["fallback"]));
  document.querySelectorAll(".model-grid [data-field]").forEach(label => {
    label.hidden = !visible.has(label.dataset.field);
  });
}

function modelMessage(text, cls) {
  const msg = document.getElementById("asr-config-msg");
  if (!msg) return;
  msg.textContent = text;
  msg.className = "msg " + (cls || "");
}

function populateModelProviders() {
  if (!modelProvider || !asrConfigData) return;
  modelProvider.innerHTML = "";
  for (const name of Object.keys(asrConfigData.providers || {})) {
    const option = document.createElement("option");
    option.value = name;
    option.textContent = name;
    modelProvider.appendChild(option);
  }
  if (asrConfigData.provider) modelProvider.value = asrConfigData.provider;
}

function fillModelFields(name) {
  if (!asrConfigData) return;
  const provider = (asrConfigData.providers || {})[name] || {};
  modelFields.type.value = provider.type || (name === "mimo" ? "mimo" : "openai");
  modelFields.model.value = provider.model || "";
  modelFields.base_url.value = provider.base_url || "";
  modelFields.api_key_file.value = provider.api_key_file || "";
  modelFields.binary.value = provider.binary || "";
  modelFields.model_path.value = provider.model_path || "";
  modelFields.threads.value = provider.threads || 0;
  modelFields.timeout_seconds.value = provider.timeout_seconds || 0;
  modelFields.language.value = provider.language || asrConfigData.language || "auto";
  modelFields.args.value = (provider.args || []).join("\n");
  document.getElementById("model-fallback").value = (asrConfigData.fallback || []).join(", ");
  syncModelFields();
}

async function loadASRConfig() {
  try {
    const res = await fetch("/api/asr/config");
    asrConfigData = await res.json();
    if (!asrConfigData.providers) asrConfigData.providers = {};
    populateModelProviders();
    fillModelFields(modelProvider.value || asrConfigData.provider);
    const active = asrConfigData.provider || "";
    const live = (asrConfigData.providers || {})[active] || {};
    modelMessage(active
      ? "当前生效：" + active + "（" + (live.model || TYPE_LABELS[live.type] || "未配置") + "）"
      : "还没有可用的识别配置", active ? "ok" : "err");
  } catch (error) {
    modelMessage("加载模型配置失败：" + error, "err");
  }
}

if (modelProvider) {
  modelProvider.addEventListener("change", () => fillModelFields(modelProvider.value));
}
if (modelFields.type) {
  modelFields.type.addEventListener("change", syncModelFields);
}

document.getElementById("btn-asr-add").addEventListener("click", () => {
  if (!asrConfigData) return;
  const name = prompt("新配置名称（例如 local-whisper）");
  if (!name) return;
  if (asrConfigData.providers[name]) {
    alert("配置名已存在");
    return;
  }
  asrConfigData.providers[name] = {
    type: "openai",
    model: "whisper-1",
    base_url: "http://127.0.0.1:8080/v1",
    language: "auto",
  };
  unsavedASR.add(name);
  asrConfigData.provider = name;
  populateModelProviders();
  modelProvider.value = name;
  fillModelFields(name);
  modelMessage("已新增 " + name + "，编辑后点保存", "ok");
});

document.getElementById("btn-asr-delete").addEventListener("click", () => {
  if (!asrConfigData || !modelProvider.value) return;
  const name = modelProvider.value;
  if (!confirm("删除配置 " + name + "？")) return;
  delete asrConfigData.providers[name];
  unsavedASR.delete(name);
  asrConfigData.fallback = (asrConfigData.fallback || []).filter(item => item !== name);
  if (asrConfigData.provider === name) {
    asrConfigData.provider = Object.keys(asrConfigData.providers)[0] || "";
  }
  populateModelProviders();
  if (asrConfigData.provider) modelProvider.value = asrConfigData.provider;
  fillModelFields(modelProvider.value);
  modelMessage("已删除 " + name + "，点保存生效", "ok");
});

document.getElementById("btn-asr-save").addEventListener("click", async () => {
  if (!asrConfigData || !modelProvider.value) {
    modelMessage("没有可保存的 Provider", "err");
    return;
  }
  const name = modelProvider.value;
  const provider = asrConfigData.providers[name] || {};
  provider.type = modelFields.type.value.trim();
  provider.model = modelFields.model.value.trim();
  provider.base_url = modelFields.base_url.value.trim();
  provider.api_key_file = modelFields.api_key_file.value.trim();
  provider.binary = modelFields.binary.value.trim();
  provider.model_path = modelFields.model_path.value.trim();
  provider.threads = Number(modelFields.threads.value) || 0;
  provider.timeout_seconds = Number(modelFields.timeout_seconds.value) || 0;
  provider.language = modelFields.language.value.trim() || "auto";
  const args = modelFields.args.value.split("\n").map(item => item.trim()).filter(Boolean);
  if (args.length) {
    provider.args = args;
  } else {
    delete provider.args;
  }
  asrConfigData.providers[name] = provider;
  asrConfigData.provider = name;
  asrConfigData.fallback = document.getElementById("model-fallback").value
    .split(",").map(item => item.trim()).filter(Boolean);
  modelMessage("正在保存…", "");
  try {
    const res = await fetch("/api/asr/config", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(asrConfigData),
    });
    const data = await res.json();
    if (!data.ok) {
      modelMessage("保存失败：" + errorText(data), "err");
      return;
    }
    modelMessage("模型配置已保存并生效", "ok");
    unsavedASR.delete(name);
    await loadStatus(false);
    await loadASRConfig();
  } catch (error) {
    modelMessage("保存失败：" + error, "err");
  }
});

document.getElementById("btn-asr-test").addEventListener("click", async () => {
  if (!modelProvider.value) return;
  if (unsavedASR.has(modelProvider.value)) {
    modelMessage("这个配置还没保存，先点「保存并启用」再检查", "err");
    return;
  }
  modelMessage("正在检查 " + modelProvider.value + " …", "");
  try {
    const res = await fetch("/api/asr/test", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({provider: modelProvider.value}),
    });
    const data = await res.json();
    const lines = (data.diagnostics || []).map(item =>
      (item.ok ? "OK  " : "FAIL") + "  [" + item.provider + "] " + item.detail
    );
    modelMessage(
      lines.join("\n") || "没有诊断结果",
      lines.some(line => line.startsWith("FAIL")) ? "err" : "ok"
    );
  } catch (error) {
    modelMessage("检查失败：" + error, "err");
  }
});

document.getElementById("subtitle-smaller").addEventListener("click", () => {
  subtitleFont = Math.max(18, subtitleFont - 2);
  applySubtitleFont();
});
document.getElementById("subtitle-larger").addEventListener("click", () => {
  subtitleFont = Math.min(64, subtitleFont + 2);
  applySubtitleFont();
});
document.getElementById("subtitle-clear").addEventListener("click", clearSubtitles);
document.getElementById("subtitle-copy").addEventListener("click", copySubtitles);
document.getElementById("subtitle-fullscreen").addEventListener("click", () => {
  if (!subtitleScene) return;
  if (document.fullscreenElement) {
    document.exitFullscreen().catch(() => {});
  } else if (subtitleScene.requestFullscreen) {
    const request = subtitleScene.requestFullscreen();
    if (request && request.catch) request.catch(() => {});
  }
});
document.getElementById("device-refresh").addEventListener("click", loadDevices);
document.getElementById("device-scan").addEventListener("click", loadDiscovery);
document.getElementById("device-reset").addEventListener("click", resetDeviceForm);
document.getElementById("device-save").addEventListener("click", saveDevice);
document.getElementById("device-delete").addEventListener("click", deleteDevice);
loadDevices();
loadInputs();
const asrPreset = document.getElementById("asr-preset");
populateASRPresets();
if (asrPreset) {
  asrPreset.addEventListener("change", () => {
    if (asrPreset.value) applyASRPreset(asrPreset.value);
  });
}

const llmProvider = document.getElementById("llm-provider");
if (llmProvider) {
  llmProvider.addEventListener("change", () => {
    const preset = llmPreset(llmProvider.value);
    if (!preset) return;
    document.getElementById("llm-base-url").value = preset.base_url || "";
    document.getElementById("llm-model").value = preset.model || "";
    document.getElementById("llm-model").placeholder = preset.model_hint || "";
    document.getElementById("llm-api-key").placeholder = preset.key_hint || "sk-...";
    llmMessage(preset.description || "", "");
  });
}

document.getElementById("btn-llm-save").addEventListener("click", async () => {
  if (!llmConfigData) await loadLLMConfig();
  const apiKey = document.getElementById("llm-api-key").value.trim();
  const payload = {
    enabled: document.getElementById("llm-enabled").checked,
    provider: document.getElementById("llm-provider").value,
    base_url: document.getElementById("llm-base-url").value.trim(),
    model: document.getElementById("llm-model").value.trim(),
    api_key_file: document.getElementById("llm-api-key-file").value.trim(),
    mode: document.getElementById("llm-mode").value,
    prompt: document.getElementById("llm-prompt").value,
    timeout_seconds: Number(document.getElementById("llm-timeout").value) || 20,
  };
  if (payload.enabled && (!payload.base_url || !payload.model)) {
    llmMessage("启用后必须填写 Base URL 和 Model", "err");
    return;
  }
  llmMessage("正在保存…", "");
  try {
    const res = await fetch("/api/llm/config", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(payload),
    });
    const data = await res.json();
    if (!data.ok) {
      llmMessage("保存失败：" + errorText(data), "err");
      return;
    }
    if (apiKey) {
      const keyRes = await fetch("/api/llm/key", {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({key: apiKey}),
      });
      const keyData = await keyRes.json();
      if (!keyData.ok) {
        llmMessage("配置已保存，但 API Key 保存失败：" + errorText(keyData), "err");
        return;
      }
      document.getElementById("llm-api-key").value = "";
    }
    llmMessage("大模型配置已保存并生效", "ok");
    await loadLLMConfig();
    await loadStatus(false);
  } catch (error) {
    llmMessage("保存失败：" + error, "err");
  }
});

async function testLLM(live) {
  const provider = document.getElementById("llm-provider").value;
  llmMessage(live ? "正在做连通测试…" : "正在检查配置…", "");
  try {
    const res = await fetch("/api/llm/test", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({provider, live}),
    });
    const data = await res.json();
    const lines = (data.diagnostics || []).map(item => (item.ok ? "OK  " : "FAIL") + "  " + item.detail);
    llmMessage(lines.join("\n") || "没有诊断结果", lines.some(line => line.startsWith("FAIL")) ? "err" : "ok");
  } catch (error) {
    llmMessage("检查失败：" + error, "err");
  }
}
document.getElementById("btn-llm-test").addEventListener("click", () => testLLM(false));
document.getElementById("btn-llm-ping").addEventListener("click", () => testLLM(true));


document.getElementById("history-refresh").addEventListener("click", loadHistory);
document.getElementById("history-search").addEventListener("input", renderHistory);

// The 保留/脱敏 controls speak to /api/history/policy; both travel together
// because the endpoint takes the pair.
async function loadHistoryPolicy() {
  const retention = document.getElementById("history-retention");
  const redact = document.getElementById("history-redact");
  if (!retention || !redact) return;
  try {
    const res = await fetch("/api/history/policy");
    const data = await res.json();
    if (data.ok) {
      retention.value = String(data.retention_days ?? 0);
      redact.checked = !!data.redact_addresses;
    }
  } catch (error) {
    historyMessage("历史策略读取失败：" + errorText(error), "err");
  }
  const save = async () => {
    try {
      const res = await fetch("/api/history/policy", {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({
          retention_days: Number(retention.value) || 0,
          redact_addresses: redact.checked,
        }),
      });
      const data = await res.json();
      historyMessage(data.ok ? "历史策略已保存并生效" : ("保存失败：" + errorText(data)), data.ok ? "ok" : "err");
    } catch (error) {
      historyMessage("保存失败：" + error, "err");
    }
  };
  retention.addEventListener("change", save);
  redact.addEventListener("change", save);
}
loadHistoryPolicy();
function syncHistoryActions() {
  const empty = !historyEntries.length;
  for (const id of ["history-export", "history-clear"]) {
    const btn = document.getElementById(id);
    if (btn) btn.disabled = empty;
  }
}

document.getElementById("history-export").addEventListener("click", () => {
  if (!historyEntries.length) {
    historyMessage("没有可导出的记录", "");
    return;
  }
  const blob = new Blob([JSON.stringify(historyEntries, null, 2)], {type: "application/json"});
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = "xiaomi-history-" + new Date().toISOString().slice(0, 10) + ".json";
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
  historyMessage("已导出 " + historyEntries.length + " 条记录", "ok");
});
document.getElementById("history-clear").addEventListener("click", async () => {
  if (!confirm("清空全部听写历史？")) return;
  const res = await fetch("/api/history/clear", {method: "POST", headers: {"Content-Type": "application/json"}, body: "{}"});
  const data = await res.json();
  if (!data.ok) {
    historyMessage("清空失败：" + errorText(data), "err");
    return;
  }
  await loadHistory();
  historyMessage("历史已清空", "ok");
});
document.getElementById("dictionary-add").addEventListener("click", () => {
  document.getElementById("dictionary-list").appendChild(dictionaryRow());
});
document.getElementById("snippet-add").addEventListener("click", () => {
  document.getElementById("snippet-list").appendChild(snippetRow());
});
function syncLLMEnabledState() {
  const toggle = document.getElementById("llm-enabled");
  const panel = document.getElementById("cfg-llm");
  if (!toggle || !panel) return;
  const on = toggle.checked;
  for (const el of panel.querySelectorAll("input, select, textarea, button")) {
    if (el.id === "llm-enabled") continue;
    el.disabled = !on;
  }
}
document.getElementById("llm-enabled")?.addEventListener("change", syncLLMEnabledState);

document.getElementById("app-rules-add")?.addEventListener("click", () => {
  document.getElementById("app-rules-list").appendChild(appRuleRow());
});
document.getElementById("app-rules-save")?.addEventListener("click", saveLLMAppRules);

document.getElementById("commands-add").addEventListener("click", () => {
  document.getElementById("commands-list").appendChild(commandRow());
});
document.getElementById("commands-defaults").addEventListener("click", () => {
  // Append the recommended pairs the user does not already have — never
  // clobber a command they wrote themselves.
  const list = document.getElementById("commands-list");
  const have = new Set();
  for (const row of list.querySelectorAll(".rule-row")) {
    have.add(row.querySelector(".rule-from").value.trim());
  }
  for (const [spoken, combo] of Object.entries(DEFAULT_COMMANDS)) {
    if (!have.has(spoken)) list.appendChild(commandRow(spoken, combo));
  }
  while (list.querySelectorAll(".rule-row").length > 1 && list.querySelector(".rule-row .rule-from").value === "") {
    list.querySelector(".rule-row").remove();
  }
});
document.getElementById("text-save").addEventListener("click", saveTextConfig);
loadTextConfig();
loadLLMConfig();
loadASRConfig();
loadLLMAppRules();
syncLLMEnabledState();
loadStreamingToggle();
loadWakeToggle();
loadLookbackToggle();
loadPhonePowerToggle();
loadSubtitles();
loadOverviewRecent();
loadStats();
syncNav();
loadStatus(true);
loadPermissions();
connectEvents();
maybeOpenWelcome();
setInterval(() => { if (!document.hidden) loadStatus(false); }, 15000);
document.addEventListener("visibilitychange", () => { if (!document.hidden) loadStatus(false); });
