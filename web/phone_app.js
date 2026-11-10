(() => {
  'use strict';

  const TARGET_RATE = 16000;
  const FRAME_SAMPLES = 320; // 20 ms at 16 kHz
  const LS_TOKEN = 'ovb.phone.token';
  const LS_DEVICE = 'ovb.phone.device';
  const LS_NAME = 'ovb.phone.name';

  const $ = (id) => document.getElementById(id);
  const state = {
    token: localStorage.getItem(LS_TOKEN) || '',
    device: localStorage.getItem(LS_DEVICE) || '',
    ws: null,
    reconnectDelay: 1000,
    reconnectTimer: 0,
    heartbeat: 0,
    mic: { stream: null, context: null, node: null, sending: false },
    resample: { ratio: 1, phase: 0, prev: 0, fresh: true },
    carry: new Int16Array(FRAME_SAMPLES),
    carryLen: 0,
    micAvailable: false,
    wake: null,
  };

  // Keys travel over the WebSocket, so they are only usable once the server
  // has greeted this device.
  function setKeysEnabled(enabled) {
    document.querySelectorAll('[data-key]').forEach((button) => { button.disabled = !enabled; });
  }

  function setStatus(text, cls) {
    const node = $('status');
    node.textContent = text;
    node.className = 'status ' + (cls || '');
  }

  function toast(text) {
    const node = $('toast');
    node.textContent = text;
    node.classList.add('show');
    clearTimeout(node.timer);
    node.timer = setTimeout(() => node.classList.remove('show'), 2600);
  }

  // --- pairing -----------------------------------------------------------

  function pairCodeFromURL() {
    const match = /(?:^|[#&])pair=([0-9]{6})/.exec(location.hash || '');
    return match ? match[1] : '';
  }

  async function pair(code) {
    const name = ($('device-name').value.trim()) || navigator.platform || 'Phone';
    const response = await fetch('/api/phone/pair', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code, name }),
    });
    const body = await response.json();
    if (!response.ok || !body.token) {
      throw new Error(body.error || '配对失败');
    }
    state.token = body.token;
    state.device = body.device_id;
    localStorage.setItem(LS_TOKEN, body.token);
    localStorage.setItem(LS_DEVICE, body.device_id);
    localStorage.setItem(LS_NAME, name);
    // The code is a secret and lives in the URL fragment: scrub it so it does
    // not linger in history or get re-submitted on the next reload.
    history.replaceState(null, '', location.pathname);
  }

  function showPairing(message) {
    $('pair').hidden = false;
    $('remote').hidden = true;
    if (message) $('pair-error').textContent = message;
  }

  // --- websocket ---------------------------------------------------------

  async function ticket() {
    const response = await fetch('/api/phone/ticket', {
      method: 'POST',
      headers: { Authorization: 'Bearer ' + state.token },
    });
    if (response.status === 401) {
      localStorage.removeItem(LS_TOKEN);
      localStorage.removeItem(LS_DEVICE);
      throw new Error('需要重新配对');
    }
    const body = await response.json();
    if (!response.ok || !body.ticket) throw new Error(body.error || '无法建立连接');
    return body.ticket;
  }

  async function connect() {
    if (!state.token) { showPairing(''); return; }
    try {
      const oneTime = await ticket();
      const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
      const ws = new WebSocket(scheme + '://' + location.host + '/ws/phone?ticket=' + encodeURIComponent(oneTime));
      state.ws = ws;
      ws.onopen = () => {
        state.reconnectDelay = 1000;
        $('device-label').textContent = state.device || '';
        startHeartbeat(ws);
      };
      ws.onmessage = (event) => typeof event.data === 'string' ? onControl(JSON.parse(event.data)) : null;
      ws.onclose = () => { stopHeartbeat(); setKeysEnabled(false); dropMic(); scheduleReconnect(); };
      ws.onerror = () => { setStatus('连接异常', 'bad'); };
    } catch (error) {
      stopHeartbeat();
      setStatus(String(error.message || error), 'bad');
      if (/重新配对/.test(String(error.message || error))) { showPairing(''); return; }
      scheduleReconnect();
    }
  }

  function onControl(frame) {
    switch (frame.type) {
      case 'ready':
        state.micAvailable = !!frame.mic_available;
        $('talk').hidden = !state.micAvailable;
        setKeysEnabled(true);
        setStatus('已连接 · ' + (frame.name || ''), 'good');
        break;
      case 'listening':
        $('talk').classList.toggle('recording', frame.active === true);
        break;
      case 'transcript':
        renderTranscript(frame);
        break;
      case 'error':
        handleFrameError(frame);
        break;
      default:
        break;
    }
  }

  function handleFrameError(frame) {
    const copy = {
      busy: '另一路语音正在输入，请先说完',
      too_long: '已达到最长录音时长',
      bad_format: '音频格式不受支持',
      bad_state: '上一句还没结束',
      no_speech: '没听清，请再说一次',
      not_ready: '本机未开启手机语音',
      denied: '按键不被允许',
    };
    toast(copy[frame.code] || (frame.message || '错误：' + frame.code));
  }

  function renderTranscript(frame) {
    const node = $('transcript');
    const latency = frame.latency_ms ? ' · ' + Math.round(frame.latency_ms / 100) / 10 + 's' : '';
    node.textContent = frame.text + latency + (frame.truncated ? ' ·（已截断）' : '');
    $('transcript').hidden = false;
  }

  function scheduleReconnect() {
    setStatus('桌面端离线，重连中…', 'warn');
    clearTimeout(state.reconnectTimer);
    state.reconnectTimer = setTimeout(connect, state.reconnectDelay);
    state.reconnectDelay = Math.min(30000, state.reconnectDelay * 2);
  }

  function startHeartbeat(ws) {
    stopHeartbeat();
    state.heartbeat = setInterval(() => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'ping', t: Date.now() }));
    }, 15000);
  }

  function stopHeartbeat() {
    clearInterval(state.heartbeat);
    state.heartbeat = 0;
  }

  // --- keys --------------------------------------------------------------

  function sendKey(key, value) {
    if (!state.ws || state.ws.readyState !== WebSocket.OPEN) { toast('未连接'); return; }
    state.ws.send(JSON.stringify({ type: 'key', key, value }));
  }

  function bindKey(button) {
    const key = button.dataset.key;
    let repeat = 0;
    let holdTimer = 0;

    const stop = () => {
      clearInterval(repeat);
      clearTimeout(holdTimer);
      repeat = 0;
      sendKey(key, 0);
    };
    const start = (event) => {
      event.preventDefault();
      if (event.pointerType === 'mouse') return;
      sendKey(key, 1);
      if (navigator.vibrate) navigator.vibrate(12);
      // Spec §5.2: long-press repeat is a phone-side behaviour, one key in
      // flight at a time, so the server keeps no repeat state.
      holdTimer = setTimeout(() => {
        repeat = setInterval(() => {
          sendKey(key, 1);
          setTimeout(() => sendKey(key, 0), 40);
        }, 120);
      }, 500);
    };
    button.addEventListener('pointerdown', start);
    ['pointerup', 'pointercancel', 'pointerleave'].forEach((name) => button.addEventListener(name, stop));
    button.addEventListener('contextmenu', (event) => event.preventDefault());
  }

  // --- microphone --------------------------------------------------------

  // Linear-interpolation resampler to 16 kHz mono. phase and prev persist
  // across blocks, so 44.1/48/96 kHz inputs stay on pitch for a whole
  // utterance instead of drifting once per block.
  function feed(floats) {
    const resample = state.resample;
    if (resample.fresh) { resample.prev = floats.length ? floats[0] : 0; resample.fresh = false; }
    for (let i = 0; i < floats.length; i++) {
      const current = floats[i];
      while (resample.phase < 1) {
        const value = resample.prev + (current - resample.prev) * resample.phase;
        pushSample(Math.max(-1, Math.min(1, value)));
        resample.phase += resample.ratio;
      }
      resample.phase -= 1;
      resample.prev = current;
    }
  }

  function pushSample(value) {
    const frame = state.carry;
    frame[state.carryLen++] = value < 0 ? value * 0x8000 : value * 0x7fff;
    if (state.carryLen < FRAME_SAMPLES) return;
    if (state.ws && state.ws.readyState === WebSocket.OPEN && state.mic.sending) {
      // Little-endian Int16, matching the server's decoder. Every browser we
      // target on Android/iOS is little-endian.
      state.ws.send(frame.slice(0).buffer);
      let peak = 0;
      for (let i = 0; i < FRAME_SAMPLES; i++) {
        const magnitude = Math.abs(frame[i]);
        if (magnitude > peak) peak = magnitude;
      }
      $('level-bar').style.width = Math.min(100, Math.round(peak / 0x4000 * 100)) + '%';
    }
    state.carryLen = 0;
  }

  async function startMic() {
    if (state.mic.sending) return;
    if (!state.ws || state.ws.readyState !== WebSocket.OPEN) { toast('未连接'); return; }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true },
      });
      state.mic.stream = stream;
      const context = new AudioContext({ latencyHint: 'interactive' });
      state.mic.context = context;
      await context.audioWorklet.addModule('/phone/worklet.js');
      const source = context.createMediaStreamSource(stream);
      const node = new AudioWorkletNode(context, 'mic-pump', {
        processorOptions: { inputRate: context.sampleRate },
      });
      state.mic.node = node;
      node.port.onmessage = (event) => feed(event.data);
      // The node needs a live output path or the graph never pulls it; the
      // zero-gain sink keeps that path from feeding the mic to the speaker.
      const mute = context.createGain();
      mute.gain.value = 0;
      source.connect(node);
      node.connect(mute);
      mute.connect(context.destination);
      state.resample = { ratio: context.sampleRate / TARGET_RATE, phase: 0, prev: 0, fresh: true };
      state.carryLen = 0;
      state.ws.send(JSON.stringify({ type: 'start', format: 'pcm16', rate: TARGET_RATE, channels: 1 }));
      state.mic.sending = true;
      if ('wakeLock' in navigator) {
        navigator.wakeLock.request('screen')
          .then((sentinel) => { state.wake = sentinel; })
          .catch(() => {});
      }
    } catch (error) {
      await stopMic(false);
      toast('需要麦克风权限才能语音输入');
    }
  }

  async function stopMic(finish = true) {
    if (state.mic.sending && finish && state.ws && state.ws.readyState === WebSocket.OPEN) {
      if (state.carryLen > 0 && state.mic.context) {
        flushPartialFrame();
      }
      state.ws.send(JSON.stringify({ type: 'end' }));
    }
    state.mic.sending = false;
    if (state.mic.node) state.mic.node.port.postMessage('stop');
    if (state.mic.stream) state.mic.stream.getTracks().forEach((track) => track.stop());
    if (state.mic.context) {
      const context = state.mic.context;
      state.mic.context = null;
      await context.close().catch(() => {});
    }
    state.mic.node = null;
    state.mic.stream = null;
    state.resample.fresh = true;
    $('level-bar').style.width = '0%';
    if (state.wake) {
      const wake = state.wake;
      state.wake = null;
      wake.release().catch(() => {});
    }
  }

  // Push the tail of an utterance so a short release is not lost between 20 ms
  // frames; the server still drops anything under its 250 ms floor.
  function flushPartialFrame() {
    const frame = state.carry;
    const pad = FRAME_SAMPLES - state.carryLen;
    for (let i = 0; i < pad; i++) frame[state.carryLen + i] = 0;
    if (state.ws && state.ws.readyState === WebSocket.OPEN) state.ws.send(frame.slice(0).buffer);
    state.carryLen = 0;
  }

  function bindTalk(button) {
    const start = (event) => {
      event.preventDefault();
      $('transcript').hidden = true;
      startMic();
    };
    const stop = (event) => {
      event.preventDefault();
      stopMic(true);
    };
    button.addEventListener('pointerdown', start);
    ['pointerup', 'pointercancel', 'pointerleave'].forEach((name) => button.addEventListener(name, stop));
    button.addEventListener('contextmenu', (event) => event.preventDefault());
  }

  // --- boot --------------------------------------------------------------

  function bindRemote() {
    document.querySelectorAll('[data-key]').forEach(bindKey);
    bindTalk($('talk'));
    $('remote').hidden = false;
    $('pair').hidden = true;
  }

  async function boot() {
    setKeysEnabled(false);
    const code = pairCodeFromURL();
    if (code) {
      try {
        await pair(code);
      } catch (error) {
        showPairing(String(error.message || error));
        return;
      }
    }
    if (!state.token) { showPairing(''); return; }
    $('device-name').value = localStorage.getItem(LS_NAME) || '';
    bindRemote();
    await connect();
  }

  $('pair-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const manual = $('pair-code').value.trim();
    if (!/^[0-9]{6}$/.test(manual)) { $('pair-error').textContent = '配对码是 6 位数字'; return; }
    try {
      await pair(manual);
      bindRemote();
      await connect();
    } catch (error) {
      $('pair-error').textContent = String(error.message || error);
    }
  });

  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') {
      if (!state.ws || state.ws.readyState !== WebSocket.OPEN) connect();
    } else if (state.mic.sending) {
      // Spec §9: backgrounding mid-speech ends the utterance rather than
      // leaving the desktop waiting for a release that never comes.
      stopMic(true);
    }
  });

  boot();
})();
