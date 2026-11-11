<script>
  import { onDestroy, tick } from 'svelte';

  export let currentProject = null;

  const MAX_BUILD_LINES = 3000;
  const MAX_LOG_LINES = 4000;
  const LEVELS = ['V', 'D', 'I', 'W', 'E', 'F'];
  const levelRank = (l) => (l === 'A' ? 5 : Math.max(0, LEVELS.indexOf(l)));

  let isOpen = false;
  let activeTab = 'build'; // build | logcat
  let showEnv = false;

  let env = null;
  let envLoading = false;
  let pkg = '';

  let pipeTask = null; // build / install / launch / run
  let buildLines = [];
  let logTask = null;
  let logLines = [];

  let logLevel = 'V';
  let logFilter = '';
  let logAll = false;
  let autoScroll = true;
  let actionMsg = '';

  let buildBox;
  let logBox;
  const streams = {}; // slot -> { es, taskId, from, timer }

  let lastProjectPath = null;
  $: if (currentProject && currentProject.path !== lastProjectPath) {
    lastProjectPath = currentProject.path;
    resetForProject();
  }

  $: privileged = env && (env.shell.mode === 'rish' || env.shell.mode === 'adb');
  $: pipeRunning = pipeTask && pipeTask.status === 'running';
  $: logRunning = logTask && logTask.status === 'running';
  $: isAndroid = env && env.project && env.project.isAndroid && env.project.appModule;
  $: blocking = env ? env.problems : [];
  $: apkURL = currentProject ? `/api/android/apk?root=${encodeURIComponent(currentProject.path)}` : '';
  $: manualInstall = pipeTask?.status === 'success' && pipeTask?.result?.manualInstall;

  $: filteredLogs = filterLogs(logLines, logLevel, logFilter);

  // 切换标签页后控制台 DOM 会重建，滚动位置需重新定位到底部
  $: activeTab, isOpen, tick().then(() => scrollToBottom(activeTab === 'logcat' ? logBox : buildBox));

  function filterLogs(lines, level, text) {
    const min = levelRank(level);
    const q = text.trim().toLowerCase();
    return lines.filter((l) => {
      if (l.meta) return true;
      if (levelRank(l.level) < min) return false;
      if (q && !(l.tag.toLowerCase().includes(q) || l.msg.toLowerCase().includes(q))) return false;
      return true;
    });
  }

  function closeAllStreams() {
    for (const slot of Object.keys(streams)) closeStream(slot);
  }

  function closeStream(slot) {
    const s = streams[slot];
    if (!s) return;
    if (s.es) s.es.close();
    if (s.timer) clearTimeout(s.timer);
    delete streams[slot];
  }

  async function resetForProject() {
    closeAllStreams();
    pipeTask = null;
    logTask = null;
    buildLines = [];
    logLines = [];
    pkg = '';
    env = null;
    await loadEnv(false);
    await reattach();
  }

  async function loadEnv(refresh) {
    if (!currentProject) return;
    envLoading = true;
    try {
      const res = await fetch(`/api/android/env?root=${encodeURIComponent(currentProject.path)}${refresh ? '&refresh=1' : ''}`);
      const data = await res.json();
      if (data.success) {
        env = data;
        if (!pkg && data.project && data.project.applicationId) pkg = data.project.applicationId;
      }
    } catch (e) {
      actionMsg = '环境检测失败: ' + e.message;
    } finally {
      envLoading = false;
    }
  }

  // 页面刷新或切回项目时，重新接入仍在运行的任务
  async function reattach() {
    if (!currentProject) return;
    try {
      const res = await fetch(`/api/task/active?root=${encodeURIComponent(currentProject.path)}`);
      const data = await res.json();
      const list = (data.tasks || []).sort((a, b) => b.startedAt - a.startedAt);
      const pipe = list.find((t) => t.kind !== 'logcat');
      const log = list.find((t) => t.kind === 'logcat' && t.status === 'running');
      if (pipe) attach(pipe);
      if (log) attach(log);
    } catch (e) {
      /* 忽略 */
    }
  }

  // ---------- 日志解析 ----------

  const threadtimeRe = /^(\d\d-\d\d \d\d:\d\d:\d\d\.\d+)\s+(\d+)\s+(\d+)\s+([VDIWEFA])\s+(.*?)\s*: ?(.*)$/;

  function parseLogcat(line) {
    const m = threadtimeRe.exec(line);
    if (!m) return { meta: true, text: line };
    return { time: m[1], pid: m[2], tid: m[3], level: m[4], tag: m[5], msg: m[6] };
  }

  function buildLineClass(l) {
    if (l.startsWith('✓')) return 'ok';
    if (l.startsWith('✗') || l.startsWith('FAILURE') || l.startsWith('e: ') || /error:|BUILD FAILED|Exception/.test(l)) return 'err';
    if (l.startsWith('⚠') || l.startsWith('w: ') || /warning:/i.test(l)) return 'warn';
    if (l.startsWith('━━━')) return 'head';
    if (l.startsWith('$ ')) return 'cmd';
    if (l.startsWith('> Task') || l.startsWith('·')) return 'dim';
    return '';
  }

  async function scrollToBottom(box) {
    if (!autoScroll) return;
    await tick();
    if (box) box.scrollTop = box.scrollHeight;
  }

  // ---------- 任务流 ----------

  function attach(task) {
    const slot = task.kind === 'logcat' ? 'log' : 'pipe';
    closeStream(slot);
    if (slot === 'log') {
      logTask = task;
      logLines = [];
    } else {
      pipeTask = task;
      buildLines = [];
    }
    openStream(slot, task.id, 0);
  }

  function openStream(slot, taskId, from) {
    const es = new EventSource(`/api/task/stream?id=${encodeURIComponent(taskId)}&from=${from}`);
    const state = { es, taskId, from, timer: null };
    streams[slot] = state;

    es.addEventListener('log', (e) => {
      state.from = +e.lastEventId || state.from;
      const lines = JSON.parse(e.data);
      if (slot === 'log') {
        const parsed = lines.map(parseLogcat);
        logLines = logLines.concat(parsed).slice(-MAX_LOG_LINES);
        scrollToBottom(logBox);
      } else {
        buildLines = buildLines.concat(lines).slice(-MAX_BUILD_LINES);
        scrollToBottom(buildBox);
      }
    });

    es.addEventListener('status', (e) => {
      const info = JSON.parse(e.data);
      if (slot === 'log') logTask = info;
      else pipeTask = info;
    });

    es.addEventListener('end', () => {
      closeStream(slot);
      if (slot === 'pipe') onPipelineDone();
    });

    // 断线后从已接收位置续传，避免 EventSource 自动重连导致日志重复
    es.onerror = () => {
      es.close();
      if (streams[slot] !== state) return;
      state.timer = setTimeout(() => {
        if (streams[slot] === state) openStream(slot, taskId, state.from);
      }, 1500);
    };
  }

  function onPipelineDone() {
    if (!pipeTask) return;
    // 一键运行成功后，自动开启应用日志（与 Android Studio 的 Run 行为一致）
    if (pipeTask.kind === 'run' && pipeTask.status === 'success' && startedHere.has(pipeTask.id) && privileged && !logRunning) {
      startLogcat();
    }
    if (pipeTask.status === 'success' && (pipeTask.kind === 'build' || pipeTask.kind === 'run')) {
      loadEnv(false);
    }
  }

  async function callTask(path, withPkg = true) {
    if (!currentProject) return null;
    actionMsg = '';
    let url = `/api/android/${path}?root=${encodeURIComponent(currentProject.path)}`;
    if (withPkg && pkg.trim()) url += `&pkg=${encodeURIComponent(pkg.trim())}`;
    try {
      const res = await fetch(url);
      const data = await res.json();
      if (data.task) {
        if (data.success) startedHere.add(data.task.id);
        attach(data.task);
      }
      if (!data.success) actionMsg = data.error || '请求失败';
      return data;
    } catch (e) {
      actionMsg = '请求失败: ' + e.message;
      return null;
    }
  }

  function runPipeline() {
    activeTab = 'build';
    callTask('run');
  }
  function build() {
    activeTab = 'build';
    callTask('build', false);
  }
  function install() {
    activeTab = 'build';
    callTask('install', false);
  }
  function downloadAPK() {
    actionMsg = '下载完成后，点击浏览器中的下载项打开 APK 并确认安装；首次需要允许浏览器安装未知应用。安装完成后可在安装器中点击「打开」。';
  }
  function launch() {
    activeTab = 'build';
    callTask('launch');
  }

  async function startLogcat() {
    activeTab = 'logcat';
    if (logRunning) await cancelTask(logTask);
    let url = `/api/android/logcat?root=${encodeURIComponent(currentProject.path)}`;
    if (logAll) url += '&all=1';
    else if (pkg.trim()) url += `&pkg=${encodeURIComponent(pkg.trim())}`;
    try {
      const res = await fetch(url);
      const data = await res.json();
      if (data.task) attach(data.task);
      if (!data.success) actionMsg = data.error || '请求失败';
    } catch (e) {
      actionMsg = '请求失败: ' + e.message;
    }
  }

  async function cancelTask(task) {
    if (!task) return;
    await fetch(`/api/task/stop?id=${encodeURIComponent(task.id)}`).catch(() => {});
  }

  async function stopApp() {
    actionMsg = '';
    try {
      const res = await fetch(`/api/android/stop?root=${encodeURIComponent(currentProject.path)}&pkg=${encodeURIComponent(pkg.trim())}`);
      const data = await res.json();
      actionMsg = data.success ? `已强制停止 ${pkg}` : data.error;
    } catch (e) {
      actionMsg = '请求失败: ' + e.message;
    }
  }

  function clearCurrent() {
    if (activeTab === 'logcat') logLines = [];
    else buildLines = [];
  }

  function togglePanel() {
    isOpen = !isOpen;
    if (isOpen) loadEnv(false);
  }

  const kindLabel = { build: '构建', install: '安装', launch: '启动', run: '运行', logcat: '日志' };
  const statusLabel = { running: '进行中', success: '成功', failed: '失败', stopped: '已停止' };
  const shellLabel = { rish: 'Shizuku', adb: 'ADB', local: '无特权' };

  // 发现由 CLI（m_s build / run / logcat 等）或其他页面启动的任务并自动接入，
  // 使 Agent 通过命令行的操作在 Web UI 中实时可见
  const startedHere = new Set();
  async function pollExternalTasks() {
    if (!currentProject || document.hidden) return;
    try {
      const res = await fetch(`/api/task/active?root=${encodeURIComponent(currentProject.path)}`);
      const data = await res.json();
      const running = (data.tasks || []).filter((t) => t.status === 'running');
      const pipe = running.find((t) => t.kind !== 'logcat');
      const log = running.find((t) => t.kind === 'logcat');
      if (pipe && (!pipeTask || pipeTask.id !== pipe.id)) attach(pipe);
      if (log && (!logTask || logTask.id !== log.id)) attach(log);
    } catch (e) {
      /* 服务暂不可用时忽略 */
    }
  }
  const pollTimer = setInterval(pollExternalTasks, 3000);

  onDestroy(() => {
    clearInterval(pollTimer);
    closeAllStreams();
  });
</script>

<button class="run-trigger {pipeRunning ? 'busy' : ''}" aria-label="运行" on:click={togglePanel}>
  <span class="icon">{pipeRunning ? '⏳' : '▶'}</span>
  <span class="label">运行</span>
</button>

{#if isOpen}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div class="sheet-backdrop" on:click={togglePanel}></div>
  <div class="run-sheet">
    <div class="sheet-header">
      <div class="title">
        <span class="proj">{currentProject ? currentProject.name : '未选择项目'}</span>
        {#if env}
          <!-- svelte-ignore a11y-click-events-have-key-events -->
          <!-- svelte-ignore a11y-no-static-element-interactions -->
          <span class="badge {privileged ? 'good' : 'weak'}" title={env.shell.detail} on:click={() => loadEnv(true)}>
            {shellLabel[env.shell.mode]}
          </span>
          <!-- svelte-ignore a11y-click-events-have-key-events -->
          <!-- svelte-ignore a11y-no-static-element-interactions -->
          <span class="badge {blocking.length ? 'bad' : 'good'}" on:click={() => (showEnv = !showEnv)}>
            环境 {blocking.length ? `✗ ${blocking.length}` : '✓'}
          </span>
        {/if}
      </div>
      <button class="close" on:click={togglePanel}>✕</button>
    </div>

    {#if envLoading && !env}
      <div class="hint-box">正在检测构建环境...</div>
    {:else if env}
      {#if showEnv || blocking.length}
        <div class="env-box">
          <div class="env-row"><span>JDK</span><span class={env.java ? 'ok' : 'err'}>{env.java ? env.java.version : '未找到'}</span></div>
          <div class="env-row"><span>Gradle</span><span class={env.gradle ? 'ok' : 'err'}>{env.gradle ? `${env.gradle.version} · ${env.gradle.path}` : '未找到'}</span></div>
          <div class="env-row"><span>SDK</span><span class={env.sdk ? 'ok' : 'err'}>{env.sdk ? `${env.sdk.path} [${env.sdk.platforms.join(', ') || '无 platform'}]` : '未找到'}</span></div>
          {#if env.termux}
            <div class="env-row"><span>aapt2</span><span class={env.aapt2 ? 'ok' : 'err'}>{env.aapt2 || '未找到'}</span></div>
          {/if}
          <div class="env-row"><span>设备通道</span><span class={privileged ? 'ok' : 'warn'}>{env.shell.detail}</span></div>
          {#each env.problems as p}
            <div class="env-msg err">✗ {p}</div>
          {/each}
          {#each env.hints as h}
            <div class="env-msg warn">ⓘ {h}</div>
          {/each}
          <button class="mini" on:click={() => loadEnv(true)}>{envLoading ? '检测中...' : '重新检测'}</button>
        </div>
      {/if}

      {#if !isAndroid}
        <div class="hint-box">当前项目不是 Android 应用项目（未找到带 com.android.application 插件的模块）。可在「文件」面板中新建 Android 项目。</div>
      {:else}
        <div class="pkg-row">
          <label for="ms-pkg">包名</label>
          <input id="ms-pkg" type="text" bind:value={pkg} placeholder="com.example.app" spellcheck="false" autocapitalize="off" />
        </div>

        <div class="actions">
          <button class="act primary" disabled={pipeRunning} on:click={runPipeline}>▶ 运行</button>
          <button class="act" disabled={pipeRunning} on:click={build}>🔨 构建</button>
          {#if privileged}
            <button class="act" disabled={pipeRunning} on:click={install}>📲 安装</button>
          {:else if !pipeRunning}
            <a class="act" href={apkURL} download on:click={downloadAPK}>📲 下载安装</a>
          {:else}
            <button class="act" disabled>📲 下载安装</button>
          {/if}
          <button class="act" disabled={pipeRunning} on:click={launch}>🚀 启动</button>
          <button class="act danger" disabled={!privileged} on:click={stopApp}>■ 停止</button>
          {#if pipeRunning}
            <button class="act danger" on:click={() => cancelTask(pipeTask)}>取消{kindLabel[pipeTask.kind]}</button>
          {/if}
        </div>
        {#if !privileged}
          <div class="action-msg">下载后从浏览器打开 APK，确认安装后点击「打开」。启用 Shizuku / ADB 后可直接安装和启动。</div>
        {/if}
      {/if}

      {#if actionMsg}
        <div class="action-msg">{actionMsg}</div>
      {/if}
    {/if}

    <div class="tabs">
      <button class="tab {activeTab === 'build' ? 'on' : ''}" on:click={() => (activeTab = 'build')}>
        构建输出
        {#if pipeTask}<span class="st {manualInstall ? 'waiting' : pipeTask.status}">{manualInstall ? 'APK 已就绪 · 待手动安装' : kindLabel[pipeTask.kind] + statusLabel[pipeTask.status]}</span>{/if}
      </button>
      <button class="tab {activeTab === 'logcat' ? 'on' : ''}" on:click={() => (activeTab = 'logcat')}>
        Logcat
        {#if logRunning}<span class="st running">实时</span>{/if}
      </button>
      <div class="tab-tools">
        <label class="chk"><input type="checkbox" bind:checked={autoScroll} />跟随</label>
        <button class="mini" on:click={clearCurrent}>清空</button>
      </div>
    </div>

    {#if activeTab === 'logcat'}
      <div class="log-tools">
        <select bind:value={logLevel}>
          {#each LEVELS as l}<option value={l}>{l}</option>{/each}
        </select>
        <input type="text" bind:value={logFilter} placeholder="过滤 Tag / 内容" spellcheck="false" autocapitalize="off" />
        <label class="chk"><input type="checkbox" bind:checked={logAll} disabled={logRunning} />全部进程</label>
        {#if logRunning}
          <button class="mini danger" on:click={() => cancelTask(logTask)}>停止</button>
        {:else}
          <button class="mini" disabled={!privileged} on:click={startLogcat}>开始</button>
        {/if}
      </div>
      <div class="console" bind:this={logBox}>
        {#if !privileged && env}
          <div class="line warn">读取应用日志需要 Shizuku (rish) 或无线调试 adb。</div>
        {/if}
        {#each filteredLogs as l}
          {#if l.meta}
            <div class="line dim">{l.text}</div>
          {:else}
            <div class="line lv-{l.level}"><span class="lt">{l.time.slice(6)}</span> <span class="lv">{l.level}</span> <span class="tag">{l.tag}</span>: {l.msg}</div>
          {/if}
        {/each}
      </div>
    {:else}
      <div class="console" bind:this={buildBox}>
        {#if buildLines.length === 0}
          <div class="line dim">点击「▶ 运行」将依次执行：构建 (assembleDebug) → 安装 → 启动，并自动打开应用日志。</div>
        {/if}
        {#each buildLines as l}
          <div class="line {buildLineClass(l)}">{l}</div>
        {/each}
      </div>
    {/if}
  </div>
{/if}

<style>
  .run-trigger {
    position: fixed;
    top: 14px;
    right: 14px;
    z-index: 110;
    background: rgba(15, 23, 42, 0.9);
    backdrop-filter: blur(12px);
    border: 1px solid rgba(51, 65, 85, 0.8);
    color: #86efac;
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
  .run-trigger.busy { color: #fbbf24; }
  .run-trigger:active { background: #1e293b; }
  @media (max-width: 340px) {
    .run-trigger .label { display: none; }
  }

  .sheet-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.4);
    z-index: 200;
  }

  .run-sheet {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 0;
    height: 78vh;
    background: rgba(11, 15, 25, 0.98);
    border-top: 1px solid rgba(51, 65, 85, 0.8);
    border-radius: 14px 14px 0 0;
    z-index: 220;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 12px calc(10px + env(safe-area-inset-bottom));
    box-sizing: border-box;
    box-shadow: 0 -8px 28px rgba(0, 0, 0, 0.7);
    animation: up 0.18s ease-out;
    user-select: text;
    text-align: left;
  }
  @keyframes up {
    from { transform: translateY(100%); }
    to { transform: translateY(0); }
  }

  .sheet-header { display: flex; align-items: center; justify-content: space-between; }
  .title { display: flex; align-items: center; gap: 6px; min-width: 0; }
  .proj { font-size: 13px; font-weight: 700; color: #f8fafc; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .badge { font-size: 10px; padding: 1px 7px; border-radius: 9px; border: 1px solid; cursor: pointer; white-space: nowrap; }
  .badge.good { color: #86efac; border-color: rgba(134, 239, 172, 0.4); }
  .badge.weak { color: #fbbf24; border-color: rgba(251, 191, 36, 0.4); }
  .badge.bad { color: #f87171; border-color: rgba(248, 113, 113, 0.5); }
  .close { background: none; border: none; color: #94a3b8; font-size: 16px; cursor: pointer; }

  .env-box, .hint-box {
    background: #0f172a;
    border: 1px solid #1e293b;
    border-radius: 8px;
    padding: 8px;
    font-size: 11px;
    color: #cbd5e1;
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 30vh;
    overflow-y: auto;
  }
  .env-row { display: flex; gap: 8px; }
  .env-row span:first-child { color: #64748b; min-width: 56px; flex-shrink: 0; }
  .env-row span:last-child { word-break: break-all; }
  .env-msg { line-height: 1.45; word-break: break-all; }
  .ok { color: #86efac; }
  .err { color: #f87171; }
  .warn { color: #fbbf24; }

  .pkg-row { display: flex; align-items: center; gap: 8px; font-size: 11px; color: #94a3b8; }
  .pkg-row input {
    flex: 1;
    min-width: 0;
    background: #0f172a;
    border: 1px solid #334155;
    border-radius: 6px;
    color: #e2e8f0;
    padding: 5px 8px;
    font-family: monospace;
    font-size: 12px;
  }

  .actions { display: flex; flex-wrap: wrap; gap: 6px; }
  .act {
    background: #1e293b;
    color: #e2e8f0;
    border: 1px solid #334155;
    border-radius: 8px;
    padding: 6px 10px;
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
  }
  .act.primary { background: #166534; border-color: #22c55e; color: #f0fdf4; }
  .act.danger { color: #fca5a5; }
  .act:disabled { opacity: 0.4; cursor: default; }
  a.act { text-decoration: none; }

  .action-msg { font-size: 11px; color: #fbbf24; word-break: break-all; }

  .tabs { display: flex; align-items: center; gap: 4px; border-bottom: 1px solid #1e293b; }
  .tab {
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    color: #94a3b8;
    font-size: 12px;
    padding: 5px 8px;
    cursor: pointer;
    display: flex;
    gap: 5px;
    align-items: center;
  }
  .tab.on { color: #f8fafc; border-bottom-color: #38bdf8; }
  .tab-tools { margin-left: auto; display: flex; gap: 6px; align-items: center; }
  .st { font-size: 9px; padding: 0 5px; border-radius: 6px; background: #1e293b; }
  .st.running { color: #38bdf8; }
  .st.success { color: #86efac; }
  .st.failed { color: #f87171; }
  .st.stopped { color: #94a3b8; }
  .st.waiting { color: #fbbf24; }

  .log-tools { display: flex; gap: 6px; align-items: center; }
  .log-tools select, .log-tools input[type='text'] {
    background: #0f172a;
    border: 1px solid #334155;
    border-radius: 6px;
    color: #e2e8f0;
    font-size: 11px;
    padding: 3px 6px;
  }
  .log-tools input[type='text'] { flex: 1; min-width: 0; }

  .chk { display: flex; align-items: center; gap: 3px; font-size: 10px; color: #94a3b8; white-space: nowrap; }
  .mini {
    background: #1e293b;
    color: #e2e8f0;
    border: 1px solid #334155;
    border-radius: 6px;
    font-size: 10px;
    padding: 2px 8px;
    cursor: pointer;
    align-self: flex-start;
  }
  .mini.danger { color: #fca5a5; }
  .mini:disabled { opacity: 0.4; }

  .console {
    flex: 1;
    min-height: 0;
    overflow: auto;
    background: #020617;
    border: 1px solid #1e293b;
    border-radius: 8px;
    padding: 6px 8px;
    font-family: ui-monospace, 'JetBrains Mono', Menlo, monospace;
    font-size: 10.5px;
    line-height: 1.5;
    color: #cbd5e1;
    -webkit-overflow-scrolling: touch;
    touch-action: pan-x pan-y;
  }
  .line { white-space: pre-wrap; word-break: break-all; }
  .line.ok { color: #86efac; }
  .line.err { color: #f87171; }
  .line.warn { color: #fbbf24; }
  .line.head { color: #38bdf8; font-weight: 700; margin-top: 4px; }
  .line.cmd { color: #67e8f9; }
  .line.dim { color: #64748b; }

  .lt { color: #475569; }
  .lv { font-weight: 700; }
  .tag { color: #a5b4fc; }
  .lv-V { color: #94a3b8; }
  .lv-D { color: #7dd3fc; }
  .lv-I { color: #cbd5e1; }
  .lv-W { color: #fbbf24; }
  .lv-E, .lv-F, .lv-A { color: #f87171; }
</style>
