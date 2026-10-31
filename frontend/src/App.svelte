<script>
  import { onMount } from 'svelte';

  let commits = [];
  let loading = true;
  let error = null;
  let selectedCommit = null;
  let copiedHash = false;

  // 经典多色霓虹调色盘 (GitKraken / Android Studio 风格)
  const PALETTE = [
    '#38bdf8', // 天蓝
    '#34d399', // 薄荷绿
    '#f472b6', // 珊瑚粉
    '#fbbf24', // 琥珀黄
    '#a78bfa', // 紫罗兰
    '#f87171', // 浅绯红
    '#4ade80'  // 翠绿
  ];

  const ROW_HEIGHT = 44;
  const LANE_WIDTH = 22;
  const NODE_RADIUS = 5;

  let graphRows = [];
  let maxLanes = 1;

  function getColor(laneIndex) {
    return PALETTE[laneIndex % PALETTE.length];
  }

  // 核心数学模型：线性扫描泳道分配与贝塞尔路由算法
  function computeGraph(commitList) {
    const lanes = [];
    const rows = [];
    let peakLanes = 1;

    for (let r = 0; r < commitList.length; r++) {
      const c = commitList[r];
      const h = c.hash;
      const parents = c.parents || [];

      // 1. 分配或继承列 (Lane)
      let col = lanes.indexOf(h);
      if (col === -1) {
        col = lanes.indexOf(null);
        if (col === -1) {
          col = lanes.length;
          lanes.push(h);
        } else {
          lanes[col] = h;
        }
      }

      if (lanes.length > peakLanes) peakLanes = lanes.length;

      // 2. 统计纵向直通穿透线 (Passing Lanes)
      const passing = [];
      for (let l = 0; l < lanes.length; l++) {
        if (l !== col && lanes[l] !== null) {
          passing.push(l);
        }
      }

      // 3. 计算指向父节点的路由连接 (Routes)
      const routes = [];
      if (parents.length > 0) {
        // 主父节点沿用本列
        lanes[col] = parents[0];
        routes.push({ fromCol: col, toCol: col, toParent: parents[0] });

        // 合并引入的次级父节点 (Merge 分叉/合并)
        for (let pIdx = 1; pIdx < parents.length; pIdx++) {
          const pHash = parents[pIdx];
          let pCol = lanes.indexOf(pHash);
          if (pCol === -1) {
            pCol = lanes.indexOf(null);
            if (pCol === -1) {
              pCol = lanes.length;
              lanes.push(pHash);
            } else {
              lanes[pCol] = pHash;
            }
          }
          routes.push({ fromCol: col, toCol: pCol, toParent: pHash });
        }
      } else {
        lanes[col] = null;
      }

      rows.push({
        commit: c,
        row: r,
        col: col,
        color: getColor(col),
        passing: passing,
        routes: routes
      });
    }

    maxLanes = Math.max(peakLanes, 1);
    return rows;
  }

  async function fetchGraph() {
    loading = true;
    error = null;
    try {
      const res = await fetch('/api/git/graph');
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      commits = await res.json();
      graphRows = computeGraph(commits);
      if (commits.length > 0 && !selectedCommit) {
        selectedCommit = commits[0];
      }
    } catch (e) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  function copyText(txt) {
    navigator.clipboard.writeText(txt);
    copiedHash = true;
    setTimeout(() => (copiedHash = false), 1500);
  }

  onMount(() => {
    fetchGraph();
  });
</script>

<div class="app-container">
  <!-- 顶栏导航 -->
  <header class="top-bar">
    <div class="brand">
      <div class="brand-icon">M</div>
      <div class="brand-text">
        <span class="title">Mobile Studio</span>
        <span class="sub">Git Topology Graph</span>
      </div>
    </div>
    <div class="actions">
      <button class="btn-refresh" on:click={fetchGraph} disabled={loading}>
        {loading ? '加载中...' : '刷新图谱'}
      </button>
    </div>
  </header>

  <!-- 主体工作区 -->
  <div class="main-content">
    {#if loading && graphRows.length === 0}
      <div class="center-msg">正在解析 Git 提交拓扑...</div>
    {:else if error}
      <div class="center-msg error">加载失败: {error}</div>
    {:else}
      <div class="table-container">
        <div class="log-table">
          {#each graphRows as row (row.commit.hash)}
            <!-- svelte-ignore a11y-click-events-have-key-events -->
            <!-- svelte-ignore a11y-no-static-element-interactions -->
            <div
              class="commit-row {selectedCommit?.hash === row.commit.hash ? 'selected' : ''}"
              style="height: {ROW_HEIGHT}px;"
              on:click={() => (selectedCommit = row.commit)}
            >
              <!-- 首列：SVG 矢量拓扑轨道 -->
              <div
                class="graph-cell"
                style="width: {maxLanes * LANE_WIDTH + 16}px; min-width: {maxLanes * LANE_WIDTH + 16}px;"
              >
                <svg
                  width={maxLanes * LANE_WIDTH + 16}
                  height={ROW_HEIGHT}
                  class="graph-svg"
                >
                  <!-- 1. 穿透线 (Passing vertical lines) -->
                  {#each row.passing as pCol}
                    <line
                      x1={pCol * LANE_WIDTH + 12}
                      y1={0}
                      x2={pCol * LANE_WIDTH + 12}
                      y2={ROW_HEIGHT}
                      stroke={getColor(pCol)}
                      stroke-width="2"
                      opacity="0.85"
                    />
                  {/each}

                  <!-- 2. 下行路由贝塞尔曲线 (Outgoing routes to parents) -->
                  {#each row.routes as route}
                    {#if route.fromCol === route.toCol}
                      <line
                        x1={route.fromCol * LANE_WIDTH + 12}
                        y1={ROW_HEIGHT / 2}
                        x2={route.toCol * LANE_WIDTH + 12}
                        y2={ROW_HEIGHT}
                        stroke={getColor(route.fromCol)}
                        stroke-width="2.2"
                      />
                    {:else}
                      <!-- 三阶贝塞尔曲线平滑过渡 (S-Curve) -->
                      <path
                        d="M {route.fromCol * LANE_WIDTH + 12} {ROW_HEIGHT / 2}
                           C {route.fromCol * LANE_WIDTH + 12} {ROW_HEIGHT * 0.9},
                             {route.toCol * LANE_WIDTH + 12} {ROW_HEIGHT * 0.6},
                             {route.toCol * LANE_WIDTH + 12} {ROW_HEIGHT}"
                        fill="none"
                        stroke={getColor(Math.max(route.fromCol, route.toCol))}
                        stroke-width="2.2"
                        opacity="0.9"
                      />
                    {/if}
                  {/each}

                  <!-- 3. 提交发光节点 (Commit Node) -->
                  <circle
                    cx={row.col * LANE_WIDTH + 12}
                    cy={ROW_HEIGHT / 2}
                    r={NODE_RADIUS}
                    fill={row.color}
                    stroke="#0f172a"
                    stroke-width="2.5"
                  />
                </svg>
              </div>

              <!-- 提交信息与分支徽标 -->
              <div class="message-cell">
                {#if row.commit.refs && row.commit.refs.length > 0}
                  <div class="badges-wrap">
                    {#each row.commit.refs as ref}
                      {#if ref.includes('HEAD')}
                        <span class="badge head">{ref}</span>
                      {:else if ref.includes('origin')}
                        <span class="badge remote">{ref}</span>
                      {:else}
                        <span class="badge local">{ref}</span>
                      {/if}
                    {/each}
                  </div>
                {/if}
                <span class="msg-text">{row.commit.message}</span>
              </div>

              <!-- 作者 -->
              <div class="author-cell">{row.commit.author}</div>

              <!-- 相对时间 -->
              <div class="date-cell">{row.commit.date}</div>

              <!-- Short Hash -->
              <div class="hash-cell">{row.commit.shortHash}</div>
            </div>
          {/each}
        </div>
      </div>

      <!-- 底部提交详情抽屉 -->
      {#if selectedCommit}
        <div class="detail-drawer">
          <div class="drawer-header">
            <span class="drawer-title">提交详情</span>
            <div class="hash-copy-btn" on:click={() => copyText(selectedCommit.hash)}>
              <code>{selectedCommit.hash}</code>
              <span class="copy-hint">{copiedHash ? '已复制！' : '复制完整 Hash'}</span>
            </div>
          </div>
          <div class="drawer-body">
            <div class="drawer-meta">
              <span><strong>作者:</strong> {selectedCommit.author}</span>
              <span><strong>时间:</strong> {selectedCommit.date}</span>
              <span><strong>父提交:</strong> {selectedCommit.parents.length ? selectedCommit.parents.map(p => p.slice(0, 7)).join(', ') : '无 (Root Commit)'}</span>
            </div>
            <div class="drawer-message">{selectedCommit.message}</div>
          </div>
        </div>
      {/if}
    {/if}
  </div>
</div>

<style>
  :global(body) {
    margin: 0;
    padding: 0;
    background-color: #0b0f19;
    color: #e2e8f0;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
    user-select: none;
    overflow: hidden;
  }

  .app-container {
    display: flex;
    flex-direction: column;
    height: 100vh;
    width: 100vw;
  }

  /* 顶栏 */
  .top-bar {
    height: 52px;
    background: #0f172a;
    border-bottom: 1px solid #1e293b;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 16px;
    box-shadow: 0 2px 8px rgba(0,0,0,0.4);
    z-index: 10;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .brand-icon {
    width: 28px;
    height: 28px;
    background: linear-gradient(135deg, #6366f1, #38bdf8);
    color: white;
    font-weight: 900;
    font-size: 16px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 6px;
  }

  .brand-text {
    display: flex;
    flex-direction: column;
  }

  .brand-text .title {
    font-weight: 700;
    font-size: 14px;
    color: #f8fafc;
  }

  .brand-text .sub {
    font-size: 10px;
    color: #94a3b8;
  }

  .btn-refresh {
    background: #1e293b;
    color: #38bdf8;
    border: 1px solid #334155;
    padding: 6px 12px;
    border-radius: 6px;
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
  }

  .btn-refresh:active {
    background: #334155;
  }

  /* 主视口 */
  .main-content {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    position: relative;
  }

  .center-msg {
    margin: auto;
    color: #64748b;
    font-size: 14px;
  }

  .center-msg.error {
    color: #f87171;
  }

  .table-container {
    flex: 1;
    overflow-y: auto;
    overflow-x: auto;
    background: #0b0f19;
  }

  .log-table {
    min-width: 680px;
    display: flex;
    flex-direction: column;
  }

  .commit-row {
    display: flex;
    align-items: center;
    border-bottom: 1px solid #141e33;
    cursor: pointer;
    transition: background 0.1s ease;
  }

  .commit-row:hover {
    background: #131d33;
  }

  .commit-row.selected {
    background: #1e293b;
    border-left: 3px solid #38bdf8;
  }

  .graph-cell {
    height: 100%;
    position: relative;
    flex-shrink: 0;
  }

  .graph-svg {
    display: block;
  }

  .message-cell {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 8px;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    padding: 0 8px;
  }

  .badges-wrap {
    display: inline-flex;
    gap: 4px;
    flex-shrink: 0;
  }

  .badge {
    font-size: 10px;
    font-weight: 700;
    padding: 2px 6px;
    border-radius: 4px;
  }

  .badge.head {
    background: #0284c7;
    color: #ffffff;
  }

  .badge.local {
    background: #059669;
    color: #ffffff;
  }

  .badge.remote {
    background: #475569;
    color: #e2e8f0;
  }

  .msg-text {
    font-size: 13px;
    color: #f1f5f9;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .author-cell {
    width: 110px;
    flex-shrink: 0;
    font-size: 12px;
    color: #94a3b8;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    padding: 0 8px;
  }

  .date-cell {
    width: 100px;
    flex-shrink: 0;
    font-size: 11px;
    color: #64748b;
    padding: 0 8px;
    white-space: nowrap;
  }

  .hash-cell {
    width: 70px;
    flex-shrink: 0;
    font-family: monospace;
    font-size: 12px;
    color: #38bdf8;
    padding-right: 12px;
    text-align: right;
  }

  /* 底部详情抽屉 */
  .detail-drawer {
    height: 120px;
    background: #0f172a;
    border-top: 1px solid #1e293b;
    padding: 10px 16px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    box-shadow: 0 -2px 10px rgba(0,0,0,0.3);
  }

  .drawer-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .drawer-title {
    font-size: 12px;
    font-weight: 700;
    color: #94a3b8;
    text-transform: uppercase;
  }

  .hash-copy-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    background: #1e293b;
    padding: 3px 8px;
    border-radius: 4px;
    cursor: pointer;
  }

  .hash-copy-btn code {
    font-size: 11px;
    color: #38bdf8;
  }

  .copy-hint {
    font-size: 10px;
    color: #cbd5e1;
  }

  .drawer-body {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .drawer-meta {
    display: flex;
    gap: 16px;
    font-size: 11px;
    color: #94a3b8;
  }

  .drawer-message {
    font-size: 13px;
    color: #f8fafc;
    font-weight: 500;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
