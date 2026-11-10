<script>
  import { onMount, onDestroy, tick } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import '@xterm/xterm/css/xterm.css';

  export let currentProject = null;

  let open = false;
  let sessions = []; // [{ id, title, cwd, exited }]
  let activeId = null;
  let errorMsg = '';
  let creating = false;

  // 粘滞修饰键（与 Termux 额外按键行为一致：按一次作用于下一个按键）
  let ctrlOn = false;
  let altOn = false;

  let fontSize = loadFontSize();
  let stageEl;
  const hosts = {}; // id -> 容器 DOM
  const rt = new Map(); // id -> { term, fit, ws, closedByUs, everConnected, retry }

  const THEME = {
    background: '#020617',
    foreground: '#cbd5e1',
    cursor: '#38bdf8',
    cursorAccent: '#020617',
    selectionBackground: 'rgba(56, 189, 248, 0.35)',
    black: '#1e293b', red: '#f87171', green: '#86efac', yellow: '#fbbf24',
    blue: '#60a5fa', magenta: '#c084fc', cyan: '#67e8f9', white: '#e2e8f0',
    brightBlack: '#475569', brightRed: '#fca5a5', brightGreen: '#bbf7d0', brightYellow: '#fde68a',
    brightBlue: '#93c5fd', brightMagenta: '#d8b4fe', brightCyan: '#a5f3fc', brightWhite: '#f8fafc'
  };

  function loadFontSize() {
    try {
      const v = +localStorage.getItem('ms_term_font');
      if (v >= 8 && v <= 28) return v;
    } catch (e) {}
    return 13;
  }

  // ---------- 会话管理 ----------

  async function loadSessions() {
    try {
      const res = await fetch('/api/term/list');
      const data = await res.json();
      sessions = (data.sessions || []).map(toSession);
      // 已在服务端关闭（如 m_s term close）的会话：释放本地 xterm 与连接
      for (const [id, r] of rt) {
        if (!sessions.find((s) => s.id === id)) {
          r.closedByUs = true;
          clearTimeout(r.retry);
          if (r.ws) r.ws.close();
          r.term.dispose();
          rt.delete(id);
        }
      }
      if (sessions.length && !sessions.find((s) => s.id === activeId)) {
        activeId = sessions[0].id;
        if (open) activate(activeId);
      }
    } catch (e) {
      errorMsg = '读取终端会话失败: ' + e.message;
    }
  }

  function toSession(info) {
    return { id: info.id, title: info.title, cwd: info.cwd, exited: info.exited };
  }

  async function createSession(cwd) {
    if (creating) return;
    creating = true;
    errorMsg = '';
    try {
      const dir = cwd || (currentProject ? currentProject.path : '');
      const res = await fetch(`/api/term/create?cwd=${encodeURIComponent(dir)}&cols=80&rows=24`);
      const data = await res.json();
      if (!data.success) throw new Error(data.error || '创建失败');
      sessions = [...sessions, toSession(data.session)];
      await activate(data.session.id);
      return data.session.id;
    } catch (e) {
      errorMsg = e.message;
    } finally {
      creating = false;
    }
  }

  async function closeSession(id, ask = true) {
    const s = sessions.find((x) => x.id === id);
    if (!s) return;
    if (ask && !s.exited && !confirm(`结束终端「${s.title}」？其中运行的进程会被终止。`)) return;
    const r = rt.get(id);
    if (r) {
      r.closedByUs = true;
      clearTimeout(r.retry);
      if (r.ws) r.ws.close();
      r.term.dispose();
      rt.delete(id);
    }
    await fetch(`/api/term/close?id=${encodeURIComponent(id)}`).catch(() => {});
    const idx = sessions.findIndex((x) => x.id === id);
    sessions = sessions.filter((x) => x.id !== id);
    if (activeId === id) {
      const next = sessions[Math.min(idx, sessions.length - 1)];
      activeId = null;
      if (next) await activate(next.id);
    }
  }

  // 已退出的会话按回车：在原目录重新启动
  async function restartSession(id) {
    const s = sessions.find((x) => x.id === id);
    if (!s) return;
    const cwd = s.cwd;
    await closeSession(id, false);
    await createSession(cwd);
  }

  async function activate(id) {
    activeId = id;
    await tick();
    const r = ensureTerm(id);
    if (!r) return;
    fitActive();
    if (open) r.term.focus();
  }

  // ---------- xterm 与 WebSocket ----------

  function ensureTerm(id) {
    if (rt.has(id)) return rt.get(id);
    const host = hosts[id];
    if (!host) return null;
    const term = new Terminal({
      fontSize,
      fontFamily: "ui-monospace, 'JetBrains Mono', 'Cascadia Mono', Menlo, 'DejaVu Sans Mono', monospace",
      theme: THEME,
      cursorBlink: true,
      scrollback: 5000,
      allowProposedApi: false
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    const r = { term, fit, ws: null, closedByUs: false, everConnected: false, retry: null, cols: 0, rows: 0 };
    rt.set(id, r);

    term.onData((data) => {
      const s = sessions.find((x) => x.id === id);
      if (s && s.exited) {
        if (data === '\r') restartSession(id);
        return;
      }
      send(id, applyModifiers(data));
    });

    connect(id);
    return r;
  }

  function connect(id) {
    const r = rt.get(id);
    if (!r || r.closedByUs) return;
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    const ws = new WebSocket(`${proto}://${location.host}/api/term/ws?id=${encodeURIComponent(id)}`);
    ws.binaryType = 'arraybuffer';
    r.ws = ws;

    ws.onopen = () => {
      // 重连时服务端会回放完整滚动缓冲，先清屏避免内容重复
      if (r.everConnected) r.term.reset();
      r.everConnected = true;
      r.cols = 0;
      sendResize(id);
    };
    ws.onmessage = (e) => {
      if (typeof e.data === 'string') {
        let msg;
        try { msg = JSON.parse(e.data); } catch { return; }
        if (msg.type === 'exit') {
          sessions = sessions.map((x) => (x.id === id ? { ...x, exited: true } : x));
          r.term.write('\x1b[90m按回车键在原目录重新启动\x1b[0m\r\n');
        } else if (msg.type === 'closed') {
          r.closedByUs = true;
        }
        return;
      }
      r.term.write(new Uint8Array(e.data));
    };
    ws.onclose = () => {
      if (r.closedByUs || !rt.has(id)) return;
      // 服务重启后会话已不存在：从列表移除；否则 1.5s 后重连
      r.retry = setTimeout(async () => {
        await loadSessions();
        if (!sessions.find((x) => x.id === id)) {
          r.closedByUs = true;
          r.term.write('\r\n\x1b[31m[会话已不存在（服务可能已重启）]\x1b[0m\r\n');
          return;
        }
        connect(id);
      }, 1500);
    };
  }

  function send(id, data) {
    const r = rt.get(id);
    if (r && r.ws && r.ws.readyState === WebSocket.OPEN && data) {
      r.ws.send(JSON.stringify({ type: 'input', data }));
    }
  }

  function sendResize(id) {
    const r = rt.get(id);
    if (!r || !r.ws || r.ws.readyState !== WebSocket.OPEN) return;
    const { cols, rows } = r.term;
    if (cols === r.cols && rows === r.rows) return;
    r.cols = cols;
    r.rows = rows;
    r.ws.send(JSON.stringify({ type: 'resize', cols, rows }));
  }

  function fitActive() {
    if (!open || !activeId) return;
    const r = rt.get(activeId);
    if (!r) return;
    try {
      r.fit.fit();
    } catch (e) {}
    sendResize(activeId);
  }

  // ---------- 修饰键与额外按键 ----------

  const CSI_FINAL = { up: 'A', down: 'B', right: 'C', left: 'D', home: 'H', end: 'F' };

  function modParam() {
    // xterm 修饰参数：1 + (Shift 1) + (Alt 2) + (Ctrl 4)
    return 1 + (altOn ? 2 : 0) + (ctrlOn ? 4 : 0);
  }

  function clearMods() {
    ctrlOn = false;
    altOn = false;
  }

  function applyModifiers(data) {
    if (!ctrlOn && !altOn) return data;
    let out = data;
    if (data.length === 1) {
      if (ctrlOn) {
        const ch = data;
        const code = ch.toUpperCase().charCodeAt(0);
        if (ch === ' ') out = '\x00';
        else if (ch === '?') out = '\x7f';
        else if (code >= 64 && code <= 95) out = String.fromCharCode(code - 64);
      }
      if (altOn) out = '\x1b' + out;
    } else {
      // 方向键等转义序列：附加修饰参数
      const m = /^\x1b[\[O]([ABCDHF])$/.exec(data);
      if (m) out = `\x1b[1;${modParam()}${m[1]}`;
      else if (altOn) out = '\x1b' + data;
    }
    clearMods();
    return out;
  }

  function keySeq(key) {
    const r = rt.get(activeId);
    const appCursor = r && r.term.modes && r.term.modes.applicationCursorKeysMode;
    if (CSI_FINAL[key]) {
      const f = CSI_FINAL[key];
      if (ctrlOn || altOn) {
        const seq = `\x1b[1;${modParam()}${f}`;
        clearMods();
        return seq;
      }
      return (appCursor ? '\x1bO' : '\x1b[') + f;
    }
    switch (key) {
      case 'esc':
        clearMods();
        return '\x1b';
      case 'tab':
        clearMods();
        return '\t';
      case 'pgup':
        clearMods();
        return '\x1b[5~';
      case 'pgdn':
        clearMods();
        return '\x1b[6~';
      default:
        return applyModifiers(key); // 普通字符：/ - | ~ 等
    }
  }

  let repeatTimer = null;
  let repeatDelay = null;

  function pressKey(e, key) {
    e.preventDefault(); // 保持 xterm 输入框焦点，避免软键盘收起
    if (!activeId) return;
    if (key === 'ctrl') {
      ctrlOn = !ctrlOn;
      return;
    }
    if (key === 'alt') {
      altOn = !altOn;
      return;
    }
    const s = sessions.find((x) => x.id === activeId);
    if (s && s.exited) return;
    const fire = () => send(activeId, keySeq(key));
    fire();
    if (CSI_FINAL[key] || key === 'pgup' || key === 'pgdn') {
      stopRepeat();
      const started = Date.now();
      repeatDelay = setTimeout(() => {
        repeatTimer = setInterval(() => (Date.now() - started > 10000 ? stopRepeat() : fire()), 60);
      }, 400);
    }
  }

  function stopRepeat() {
    clearTimeout(repeatDelay);
    clearInterval(repeatTimer);
    repeatDelay = repeatTimer = null;
  }

  const ROW1 = [['ESC', 'esc'], ['/', '/'], ['-', '-'], ['HOME', 'home'], ['↑', 'up'], ['END', 'end'], ['PGUP', 'pgup']];
  const ROW2 = [['TAB', 'tab'], ['CTRL', 'ctrl'], ['ALT', 'alt'], ['←', 'left'], ['↓', 'down'], ['→', 'right'], ['PGDN', 'pgdn']];

  // ---------- 工具栏 ----------

  async function paste() {
    const r = rt.get(activeId);
    if (!r) return;
    try {
      const text = await navigator.clipboard.readText();
      if (text) r.term.paste(text); // 自动处理 bracketed paste 模式
    } catch (e) {
      errorMsg = '无法读取剪贴板：' + e.message;
    }
    r.term.focus();
  }

  function setFont(delta) {
    fontSize = Math.min(28, Math.max(8, fontSize + delta));
    try {
      localStorage.setItem('ms_term_font', String(fontSize));
    } catch (e) {}
    for (const r of rt.values()) r.term.options.fontSize = fontSize;
    fitActive();
  }

  async function toggleOpen() {
    open = !open;
    if (!open) return;
    errorMsg = '';
    await loadSessions();
    if (sessions.length === 0) await createSession();
    else await activate(activeId || sessions[0].id);
  }

  function basename(p) {
    if (!p) return '';
    const parts = p.split('/').filter(Boolean);
    return parts[parts.length - 1] || '/';
  }

  // 终端视图打开时定期同步会话列表，使 m_s term new / close 的变化即时可见
  const sessionPoll = setInterval(() => {
    if (open && !document.hidden) loadSessions();
  }, 3000);

  let ro;
  onMount(() => {
    ro = new ResizeObserver(() => fitActive());
    if (stageEl) ro.observe(stageEl);
    window.addEventListener('pointerup', stopRepeat);
    window.addEventListener('pointercancel', stopRepeat);
    window.addEventListener('blur', stopRepeat);
    document.addEventListener('visibilitychange', stopRepeat);
  });

  onDestroy(() => {
    if (ro) ro.disconnect();
    clearInterval(sessionPoll);
    stopRepeat();
    window.removeEventListener('pointerup', stopRepeat);
    window.removeEventListener('pointercancel', stopRepeat);
    window.removeEventListener('blur', stopRepeat);
    document.removeEventListener('visibilitychange', stopRepeat);
    for (const r of rt.values()) {
      r.closedByUs = true;
      clearTimeout(r.retry);
      if (r.ws) r.ws.close();
      r.term.dispose();
    }
  });
</script>

<button class="term-trigger {open ? 'active' : ''}" aria-label="终端" on:click={toggleOpen}>
  <span class="icon">⌨</span>
  <span class="label">终端</span>
</button>

<div class="term-view" class:hidden={!open}>
  <div class="term-header">
    <div class="tabs">
      {#each sessions as s (s.id)}
        <div class="tab {s.id === activeId ? 'on' : ''} {s.exited ? 'exited' : ''}" title={s.cwd}>
          <button class="tab-name" on:pointerdown|preventDefault on:mousedown|preventDefault on:click={() => activate(s.id)}>
            {s.title}<span class="tab-cwd">{basename(s.cwd)}</span>
          </button>
          <button class="tab-x" aria-label="关闭会话" on:click={() => closeSession(s.id)}>×</button>
        </div>
      {/each}
      <button class="tab-add" disabled={creating} aria-label="新建终端" on:pointerdown|preventDefault on:mousedown|preventDefault on:click={() => createSession()}>＋</button>
    </div>
    <div class="tools">
      <button class="tool" on:pointerdown|preventDefault on:mousedown|preventDefault on:click={() => setFont(-1)} aria-label="缩小字号">A−</button>
      <button class="tool" on:pointerdown|preventDefault on:mousedown|preventDefault on:click={() => setFont(1)} aria-label="放大字号">A+</button>
      <button class="tool" on:pointerdown|preventDefault on:mousedown|preventDefault on:click={paste}>粘贴</button>
      <button class="tool back" on:click={toggleOpen}>返回</button>
    </div>
  </div>

  {#if errorMsg}
    <div class="term-error">{errorMsg}</div>
  {/if}

  <div class="stage" bind:this={stageEl}>
    {#each sessions as s (s.id)}
      <div class="host" class:shown={s.id === activeId} bind:this={hosts[s.id]}></div>
    {/each}
    {#if sessions.length === 0 && !creating}
      <div class="empty">没有终端会话，点击 ＋ 新建</div>
    {/if}
  </div>

  <div class="extra-keys">
    {#each [ROW1, ROW2] as row}
      <div class="key-row">
        {#each row as [label, key]}
          <button
            class="key"
            class:on={(key === 'ctrl' && ctrlOn) || (key === 'alt' && altOn)}
            on:pointerdown={(e) => pressKey(e, key)}
            on:pointerup={stopRepeat}
            on:pointerleave={stopRepeat}
            on:pointercancel={stopRepeat}
            on:mousedown|preventDefault
            on:contextmenu|preventDefault
          >{label}</button>
        {/each}
      </div>
    {/each}
  </div>
</div>

<style>
  .term-trigger {
    position: fixed;
    top: 14px;
    right: 86px;
    z-index: 110;
    background: rgba(15, 23, 42, 0.9);
    backdrop-filter: blur(12px);
    border: 1px solid rgba(51, 65, 85, 0.8);
    color: #a5b4fc;
    padding: 4px 10px;
    border-radius: 18px;
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: 11px;
    font-weight: 600;
    cursor: pointer;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.5);
  }
  .term-trigger:active { background: #1e293b; }
  /* 手机竖屏仅显示图标，避免与顶部居中的缩放胶囊重叠 */
  @media (max-width: 429px) {
    .term-trigger .label { display: none; }
  }
  /* 极窄屏「运行」按钮也收为图标，终端按钮随之右移 */
  @media (max-width: 340px) {
    .term-trigger { right: 58px; }
  }

  .term-view {
    position: fixed;
    inset: 0;
    z-index: 150;
    background: #020617;
    display: flex;
    flex-direction: column;
    text-align: left;
    padding-bottom: env(safe-area-inset-bottom);
  }
  .term-view.hidden { display: none; }

  .term-header {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 8px;
    background: #0b1120;
    border-bottom: 1px solid #1e293b;
    flex-shrink: 0;
  }

  .tabs {
    flex: 1;
    min-width: 0;
    display: flex;
    gap: 4px;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .tabs::-webkit-scrollbar { display: none; }

  .tab {
    display: flex;
    align-items: center;
    flex-shrink: 0;
    background: #111827;
    border: 1px solid #1e293b;
    border-radius: 8px;
  }
  .tab.on { border-color: #38bdf8; background: #0c1a2e; }
  .tab.exited .tab-name { color: #64748b; text-decoration: line-through; }
  .tab-name {
    background: none;
    border: none;
    color: #e2e8f0;
    font-size: 11px;
    padding: 4px 4px 4px 8px;
    cursor: pointer;
    display: flex;
    gap: 5px;
    align-items: baseline;
  }
  .tab-cwd { color: #64748b; font-size: 10px; }
  .tab-x {
    background: none;
    border: none;
    color: #64748b;
    font-size: 14px;
    padding: 0 7px 0 2px;
    cursor: pointer;
  }
  .tab-add {
    flex-shrink: 0;
    background: #111827;
    border: 1px dashed #334155;
    color: #94a3b8;
    border-radius: 8px;
    font-size: 12px;
    padding: 2px 9px;
    cursor: pointer;
  }

  .tools { display: flex; gap: 4px; flex-shrink: 0; }
  .tool {
    background: #1e293b;
    border: 1px solid #334155;
    color: #e2e8f0;
    border-radius: 6px;
    font-size: 11px;
    padding: 3px 7px;
    cursor: pointer;
  }
  .tool.back { color: #38bdf8; }

  .term-error {
    font-size: 11px;
    color: #fbbf24;
    padding: 4px 10px;
    background: #0b1120;
  }

  .stage {
    position: relative;
    flex: 1;
    min-height: 0;
    padding: 4px 0 0 6px;
  }
  .host {
    position: absolute;
    inset: 4px 0 0 6px;
    display: none;
  }
  .host.shown { display: block; }
  .empty {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #475569;
    font-size: 12px;
  }

  .extra-keys {
    flex-shrink: 0;
    background: #0b1120;
    border-top: 1px solid #1e293b;
    padding: 2px 0;
    touch-action: manipulation;
    user-select: none;
    -webkit-user-select: none;
  }
  .key-row { display: flex; }
  .key {
    flex: 1;
    background: none;
    border: none;
    color: #e2e8f0;
    font-size: 12px;
    font-family: ui-monospace, Menlo, monospace;
    padding: 9px 0;
    cursor: pointer;
    -webkit-tap-highlight-color: transparent;
  }
  .key:active { background: #1e293b; }
  .key.on { color: #0f172a; background: #38bdf8; border-radius: 4px; }
</style>
