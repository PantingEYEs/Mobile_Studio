<script>
  import { onMount } from 'svelte';

  export let node = null; // 用于递归
  export let selectedPath = null;
  export let isRoot = false;

  let rootTree = null;
  let isOpen = false; // 左侧面板默认关闭
  let expandedDirs = {}; // 记录展开的目录路径，默认全空（全部折叠）

  // 获取文件树数据
  async function fetchTree() {
    try {
      const res = await fetch('/api/fs/tree');
      if (res.ok) {
        rootTree = await res.json();
      }
    } catch (e) {
      console.error('Failed to load file tree:', e);
    }
  }

  function togglePanel() {
    isOpen = !isOpen;
    if (isOpen && !rootTree) {
      fetchTree();
    }
  }

  function toggleDir(path, e) {
    e.stopPropagation();
    expandedDirs[path] = !expandedDirs[path];
    expandedDirs = { ...expandedDirs }; // 触发 Svelte 响应式更新
  }

  function selectFile(path, e) {
    e.stopPropagation();
    selectedPath = path;
  }

  onMount(() => {
    if (isRoot) fetchTree();
  });
</script>

{#if isRoot}
  <!-- 1. 左上角轻量悬浮触发按钮 -->
  <button class="panel-trigger-btn {isOpen ? 'active' : ''}" on:click={togglePanel}>
    <span class="icon">📁</span>
    <span class="text">文件</span>
  </button>

  <!-- 2. 滑动抽屉与半透明遮罩 -->
  {#if isOpen}
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div class="panel-backdrop" on:click={togglePanel}></div>
    <div class="file-panel" on:click|stopPropagation>
      <div class="panel-header">
        <div class="header-title">
          <span class="proj-icon">📦</span>
          <span class="proj-name">{rootTree ? rootTree.name : 'Mobile_Studio'}</span>
        </div>
        <div class="header-actions">
          <button class="icon-btn" on:click={fetchTree} title="刷新目录">🔄</button>
          <button class="icon-btn close" on:click={togglePanel}>✕</button>
        </div>
      </div>

      <div class="tree-container">
        {#if rootTree && rootTree.children}
          {#each rootTree.children as child}
            <svelte:self
              node={child}
              bind:selectedPath
              bind:expandedDirs
              isRoot={false}
              on:selectFile
            />
          {/each}
        {:else}
          <div class="loading-hint">正在读取文件树...</div>
        {/if}
      </div>
    </div>
  {/if}
{:else if node}
  <!-- 递归子节点渲染层 -->
  <div class="tree-node">
    {#if node.isDir}
      <!-- 文件夹行 (默认完全折叠) -->
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="node-row dir" on:click={(e) => toggleDir(node.path, e)}>
        <span class="arrow">{expandedDirs[node.path] ? '▼' : '▶'}</span>
        <span class="folder-icon">{expandedDirs[node.path] ? '📂' : '📁'}</span>
        <span class="name">{node.name}</span>
      </div>

      {#if expandedDirs[node.path] && node.children}
        <div class="children-block">
          {#each node.children as child}
            <svelte:self
              node={child}
              bind:selectedPath
              bind:expandedDirs
              isRoot={false}
              on:selectFile
            />
          {/each}
        </div>
      {/if}
    {:else}
      <!-- 普通文件行 (点击高亮反馈，暂不提供额外交互) -->
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
  /* 悬浮唤起按钮 */
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

  /* 抽屉侧边栏 */
  .file-panel {
    position: fixed;
    top: 0;
    left: 0;
    bottom: 0;
    width: 260px;
    max-width: 80vw;
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

  /* 树形列表 */
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

  /* 普通文件选中态高亮反馈 */
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

  /* 小巧色彩文件类型圆点 */
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
