<script>
  import { onMount } from 'svelte';

  export let node = null;
  export let selectedPath = null;
  export let isRoot = false;

  export let expandedDirs = {};
  export let loadingDirs = {};

  let rootNodes = [];
  let isOpen = false;
  let rootLoading = false;

  // 常规意义上的忽略目录（在界面上标灰提醒，但完全支持点开查看）
  const IGNORED_NAMES = new Set(['.git', 'node_modules', 'bin', 'dist', '.idea', '.vscode', '.svelte-kit']);

  async function fetchChildren(dirPath) {
    try {
      const res = await fetch('/api/fs/tree?path=' + encodeURIComponent(dirPath));
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      return data.children || [];
    } catch (e) {
      console.error('Failed to fetch layer:', dirPath, e);
      return [];
    }
  }

  async function loadRoot() {
    rootLoading = true;
    rootNodes = await fetchChildren('');
    rootLoading = false;
  }

  function togglePanel() {
    isOpen = !isOpen;
    if (isOpen && rootNodes.length === 0) {
      loadRoot();
    }
  }

  // 核心：单层惰性加载触发
  async function toggleDir(dirNode, e) {
    e.stopPropagation();
    const p = dirNode.path;

    // 如果已经展开，则收起
    if (expandedDirs[p]) {
      expandedDirs[p] = false;
      expandedDirs = { ...expandedDirs };
      return;
    }

    // 如果该目录尚未加载过子项，向后端请求仅仅这一层！
    if (!dirNode.children) {
      loadingDirs[p] = true;
      loadingDirs = { ...loadingDirs };

      const children = await fetchChildren(p);
      dirNode.children = children;

      loadingDirs[p] = false;
      loadingDirs = { ...loadingDirs };
    }

    expandedDirs[p] = true;
    expandedDirs = { ...expandedDirs };
  }

  function selectFile(path, e) {
    e.stopPropagation();
    selectedPath = path;
  }

  onMount(() => {
    if (isRoot) loadRoot();
  });
</script>

{#if isRoot}
  <!-- 悬浮触发按钮 -->
  <button class="panel-trigger-btn {isOpen ? 'active' : ''}" on:click={togglePanel}>
    <span class="icon">📁</span>
    <span class="text">文件</span>
  </button>

  {#if isOpen}
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div class="panel-backdrop" on:click={togglePanel}></div>
    <div class="file-panel" on:click|stopPropagation>
      <div class="panel-header">
        <div class="header-title">
          <span class="proj-icon">📦</span>
          <span class="proj-name">Mobile_Studio</span>
        </div>
        <div class="header-actions">
          <button class="icon-btn" on:click={loadRoot} title="刷新根目录">🔄</button>
          <button class="icon-btn close" on:click={togglePanel}>✕</button>
        </div>
      </div>

      <div class="tree-container">
        {#if rootLoading}
          <div class="loading-hint">正在读取根目录...</div>
        {:else if rootNodes.length > 0}
          {#each rootNodes as child}
            <svelte:self
              node={child}
              bind:selectedPath
              bind:expandedDirs
              bind:loadingDirs
              isRoot={false}
            />
          {/each}
        {:else}
          <div class="loading-hint">目录为空</div>
        {/if}
      </div>
    </div>
  {/if}
{:else if node}
  <div class="tree-node">
    {#if node.isDir}
      <!-- 文件夹节点 -->
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="node-row dir {IGNORED_NAMES.has(node.name) ? 'ignored-dir' : ''}"
           on:click={(e) => toggleDir(node, e)}>
        <span class="arrow">{expandedDirs[node.path] ? '▼' : '▶'}</span>
        <span class="folder-icon">{expandedDirs[node.path] ? '📂' : '📁'}</span>
        <span class="name">{node.name}</span>

        {#if IGNORED_NAMES.has(node.name)}
          <span class="ignore-tag">ignore</span>
        {/if}

        {#if loadingDirs[node.path]}
          <span class="layer-loading">加载中...</span>
        {/if}
      </div>

      <!-- 子项容器 -->
      {#if expandedDirs[node.path]}
        <div class="children-block">
          {#if node.children && node.children.length > 0}
            {#each node.children as child}
              <svelte:self
                node={child}
                bind:selectedPath
                bind:expandedDirs
                bind:loadingDirs
                isRoot={false}
              />
            {/each}
          {:else if !loadingDirs[node.path]}
            <div class="empty-hint">(空文件夹)</div>
          {/if}
        </div>
      {/if}
    {:else}
      <!-- 普通文件 -->
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div
        class="node-row file {selectedPath === node.path ? 'selected' : ''}"
        on:click={(e) => selectFile(node.path, e)}
      >
        <span class="file-icon">
          {#if node.name.endsWith('.go')}
            <span class="dot go"></span>
          {:else if node.name.endsWith('.svelte')}
            <span class="dot svelte"></span>
          {:else if node.name.endsWith('.md')}
            <span class="dot md"></span>
          {:else}
            <span class="dot default"></span>
          {/if}
        </span>
        <span class="name">{node.name}</span>
      </div>
    {/if}
  </div>
{/if}

<style>
  .panel-trigger-btn {
    position: absolute;
    top: 14px;
    left: 14px;
    z-index: 110;
    background: rgba(15, 23, 42, 0.9);
    backdrop-filter: blur(12px);
    border: 1px solid rgba(51, 65, 85, 0.8);
    color: #cbd5e1;
    padding: 4px 10px;
    border-radius: 18px;
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: 11px;
    font-weight: 600;
    cursor: pointer;
    box-shadow: 0 4px 12px rgba(0,0,0,0.5);
  }

  .panel-trigger-btn:active {
    background: #1e293b;
    color: #38bdf8;
  }

  .panel-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.4);
    z-index: 200;
    backdrop-filter: blur(2px);
  }

  .file-panel {
    position: fixed;
    top: 0;
    left: 0;
    bottom: 0;
    width: 270px;
    max-width: 82vw;
    background: rgba(11, 15, 25, 0.96);
    backdrop-filter: blur(16px);
    border-right: 1px solid rgba(51, 65, 85, 0.7);
    z-index: 210;
    display: flex;
    flex-direction: column;
    box-shadow: 4px 0 24px rgba(0,0,0,0.7);
    animation: slideIn 0.18s ease-out;
  }

  @keyframes slideIn {
    from { transform: translateX(-100%); }
    to { transform: translateX(0); }
  }

  .panel-header {
    height: 48px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 12px;
    border-bottom: 1px solid #1e293b;
    background: rgba(15, 23, 42, 0.6);
  }

  .header-title {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    font-weight: 700;
    color: #f8fafc;
  }

  .header-actions {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .icon-btn {
    background: transparent;
    border: none;
    color: #94a3b8;
    font-size: 13px;
    width: 26px;
    height: 26px;
    border-radius: 4px;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
  }

  .icon-btn:active {
    background: #1e293b;
    color: #f8fafc;
  }

  .tree-container {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 8px 4px;
  }

  .loading-hint {
    padding: 16px;
    font-size: 11px;
    color: #64748b;
    text-align: center;
  }

  .tree-node {
    display: flex;
    flex-direction: column;
  }

  .node-row {
    height: 28px;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 8px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 12px;
    color: #cbd5e1;
    transition: background 0.1s;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .node-row:hover {
    background: #131d33;
  }

  .node-row.dir {
    font-weight: 600;
    color: #e2e8f0;
  }

  .node-row.dir.ignored-dir {
    color: #64748b;
  }

  .ignore-tag {
    font-size: 8px;
    color: #475569;
    border: 1px solid #334155;
    padding: 0 3px;
    border-radius: 3px;
    margin-left: 4px;
  }

  .layer-loading {
    font-size: 9px;
    color: #38bdf8;
    margin-left: auto;
    font-style: italic;
  }

  .empty-hint {
    font-size: 10px;
    color: #475569;
    padding: 4px 8px;
    font-style: italic;
  }

  .arrow {
    font-size: 9px;
    color: #64748b;
    width: 10px;
    text-align: center;
  }

  .folder-icon {
    font-size: 12px;
  }

  .name {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .node-row.file.selected {
    background: #1e293b;
    color: #38bdf8;
    font-weight: 600;
    box-shadow: inset 2px 0 0 #38bdf8;
  }

  .children-block {
    padding-left: 14px;
    border-left: 1px solid rgba(51, 65, 85, 0.4);
    margin-left: 12px;
  }

  .file-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 12px;
  }

  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
  }

  .dot.go { background: #00add8; }
  .dot.svelte { background: #ff3e00; }
  .dot.md { background: #a78bfa; }
  .dot.default { background: #64748b; }
</style>
