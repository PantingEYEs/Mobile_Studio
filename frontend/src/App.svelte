<script>
  import { onMount } from 'svelte';

  let branches = [];
  let selectedSpine = '';
  let commits = [];
  let loading = true;
  let error = null;

  // 选中节点状态 (点击交互)
  let selectedHash = null;
  let copiedHash = false;

  // 平移与缩放 (主轴锚定于正中)
  let zoom = 1.0;
  let offsetX = 0;
  let offsetY = 80;
  let isDragging = false;
  let startX = 0;
  let startY = 0;
  let startOffsetX = 0;
  let startOffsetY = 0;

  // 极限紧凑几何参数
  const ROW_GAP = 50;       // 纵向提交间距
  const LANE_SPREAD = 22;   // 紧凑横向间距 (仅22px，紧贴主轴)

  const SPINE_COLOR = '#38bdf8'; // 主干：纯澈冰蓝
  const BRANCH_COLORS = [
    '#34d399', // 薄荷绿
    '#f472b6', // 珊瑚粉
    '#fbbf24', // 琥珀黄
    '#a78bfa', // 紫罗兰
    '#fb7185'  // 玫瑰红
  ];

  let renderedNodes = [];
  let renderedEdges = [];

  // 递归树形泳道定侧算法：同分支同侧，子分支在同侧向外递归
  function layoutTopology(commitList, spineBranchName) {
    if (!commitList || commitList.length === 0) return;

    const commitMap = new Map();
    commitList.forEach(c => commitMap.set(c.hash, c));

    // 1. 定位主轴 Head 并沿 first-parent 溯源锁定主干
    let spineHeadHash = null;
    for (const c of commitList) {
      if (c.refs && c.refs.some(r => r === spineBranchName || r.endsWith('/' + spineBranchName) || r.includes('-> ' + spineBranchName))) {
        spineHeadHash = c.hash;
        break;
      }
    }
    if (!spineHeadHash && commitList.length > 0) spineHeadHash = commitList[0].hash;

    const spineCommitHashes = new Set();
    let cur = spineHeadHash;
    while (cur && commitMap.has(cur)) {
      spineCommitHashes.add(cur);
      const node = commitMap.get(cur);
      if (node.parents && node.parents.length > 0) {
        cur = node.parents[0];
      } else {
        break;
      }
    }

    // 2. 识别分支链条 (Chain)：同一分支的所有 Commit 共享相同的分支根
    const chainRootMap = new Map();
    commitList.forEach(c => {
      if (spineCommitHashes.has(c.hash)) return;
      let walk = c.hash;
      while (walk && !spineCommitHashes.has(walk) && commitMap.has(walk)) {
        const parents = commitMap.get(walk).parents;
        if (parents && parents.length > 0) {
          const nextP = parents[0];
          if (!spineCommitHashes.has(nextP)) {
            walk = nextP;
            continue;
          }
        }
        break;
      }
      chainRootMap.set(c.hash, walk);
    });

    // 3. 为每个独立分支链分配稳定的侧翼泳道与色彩（绝不跨侧横跳）
    const chainLaneMap = new Map();
    let nextRightLane = 1;
    let nextLeftLane = -1;
    let colorIdx = 0;

    commitList.forEach(c => {
      if (spineCommitHashes.has(c.hash)) return;
      const root = chainRootMap.get(c.hash) || c.hash;
      if (!chainLaneMap.has(root)) {
        // 奇偶轮替主轴两侧，后续同分支严格锁定在同侧
        let lane;
        if (Math.abs(nextLeftLane) <= nextRightLane) {
          lane = nextLeftLane--;
        } else {
          lane = nextRightLane++;
        }
        const color = BRANCH_COLORS[colorIdx % BRANCH_COLORS.length];
        colorIdx++;
        chainLaneMap.set(root, { lane, color });
      }
    });

    // 4. 计算每个提交节点的物理坐标 (X, Y)
    const nodeCoords = new Map();
    renderedNodes = commitList.map((c, index) => {
      const isSpine = spineCommitHashes.has(c.hash);
      let laneIndex = 0;
      let color = SPINE_COLOR;

      if (!isSpine) {
        const root = chainRootMap.get(c.hash) || c.hash;
        const info = chainLaneMap.get(root) || { lane: 1, color: BRANCH_COLORS[0] };
        laneIndex = info.lane;
        color = info.color;
      }

      const x = laneIndex * LANE_SPREAD;
      const y = index * ROW_GAP;
      nodeCoords.set(c.hash, { x, y, color, isSpine });

      return {
        commit: c,
        x,
        y,
        color,
        isSpine
      };
    });

    // 5. 绘制真实 Git 边（从父节点圆心平滑连接到子节点圆心）
    renderedEdges = [];
    commitList.forEach(c => {
      const from = nodeCoords.get(c.hash);
      if (!from || !c.parents) return;

      c.parents.forEach((pHash, pIndex) => {
        const to = nodeCoords.get(pHash);
        if (!to) return;

        let edgeColor = from.isSpine && to.isSpine && pIndex === 0 ? SPINE_COLOR : from.color;

        renderedEdges.push({
          id: `${c.hash}->${pHash}`,
          fromX: from.x,
          fromY: from.y,
          toX: to.x,
          toY: to.y,
          color: edgeColor,
          isStraight: from.x === to.x
        });
      });
    });
  }

  async function loadData() {
    loading = true;
    error = null;
    try {
      const res = await fetch('/api/git/graph');
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      branches = data.branches || [];
      commits = data.commits || [];

      if (!selectedSpine) {
        if (branches.includes('0.0.1-dev')) {
          selectedSpine = '0.0.1-dev';
        } else if (data.currentBranch) {
          selectedSpine = data.currentBranch;
        } else if (branches.length > 0) {
          selectedSpine = branches[0];
        }
      }

      layoutTopology(commits, selectedSpine);
    } catch (e) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  function handleSpineChange(e) {
    selectedSpine = e.target.value;
    layoutTopology(commits, selectedSpine);
  }

  // 点击选择节点交互
  function selectCommit(node) {
    if (selectedHash === node.commit.hash) {
      selectedHash = null; // 再次点击取消
    } else {
      selectedHash = node.commit.hash;
    }
  }

  function copyText(txt, e) {
    e.stopPropagation();
    navigator.clipboard.writeText(txt);
    copiedHash = true;
    setTimeout(() => (copiedHash = false), 1500);
  }

  function zoomIn() {
    zoom = Math.min(1.8, +(zoom + 0.15).toFixed(2));
  }
  function zoomOut() {
    zoom = Math.max(0.5, +(zoom - 0.15).toFixed(2));
  }
  function resetView() {
    zoom = 1.0;
    offsetX = 0;
    offsetY = 80;
  }

  // 平移拖拽
  function onTouchStart(e) {
    isDragging = true;
    const clientX = e.touches ? e.touches[0].clientX : e.clientX;
    const clientY = e.touches ? e.touches[0].clientY : e.clientY;
    startX = clientX;
    startY = clientY;
    startOffsetX = offsetX;
    startOffsetY = offsetY;
  }
  function onTouchMove(e) {
    if (!isDragging) return;
    const clientX = e.touches ? e.touches[0].clientX : e.clientX;
    const clientY = e.touches ? e.touches[0].clientY : e.clientY;
    offsetX = startOffsetX + (clientX - startX);
    offsetY = startOffsetY + (clientY - startY);
  }
  function onTouchEnd() {
    isDragging = false;
  }

  onMount(() => {
    loadData();
  });
</script>

<div class="viewport"
     on:mousedown={onTouchStart}
     on:mousemove={onTouchMove}
     on:mouseup={onTouchEnd}
     on:touchstart={onTouchStart}
     on:touchmove={onTouchMove}
     on:touchend={onTouchEnd}>

  <!-- 顶部轻量浮动控制栏 -->
  <div class="control-panel" on:mousedown|stopPropagation on:touchstart|stopPropagation>
    <div class="spine-selector-wrap">
      <span class="label">主轴:</span>
      <select class="spine-select" bind:value={selectedSpine} on:change={handleSpineChange}>
        {#each branches as b}
          <option value={b}>{b}</option>
        {/each}
      </select>
    </div>

    <div class="zoom-tools">
      <button class="btn" on:click={zoomOut}>-</button>
      <span class="zoom-value">{Math.round(zoom * 100)}%</span>
      <button class="btn" on:click={zoomIn}>+</button>
      <button class="btn reset" on:click={resetView}>居中</button>
    </div>
  </div>

  {#if loading}
    <div class="status-msg">正在梳理拓扑树...</div>
  {:else if error}
    <div class="status-msg error">{error}</div>
  {:else}
    <!-- 拓扑画布：居中展示，纯净视觉主角 -->
    <div class="canvas-transform"
         style="transform: translate(calc(-50% + {offsetX}px), {offsetY}px) scale({zoom});">

      <!-- 居中垂线参考光效 -->
      <div class="spine-center-guide"></div>

      <svg class="graph-svg" width="900" height={commits.length * ROW_GAP + 160}>
        <g transform="translate(450, 40)">
          <!-- 1. 全真贝塞尔连线 -->
          {#each renderedEdges as e (e.id)}
            {#if e.isStraight}
              <line
                x1={e.fromX}
                y1={e.fromY}
                x2={e.toX}
                y2={e.toY}
                stroke={e.color}
                stroke-width="2.6"
                opacity="0.85"
              />
            {:else}
              <path
                d="M {e.fromX} {e.fromY}
                   C {e.fromX} {e.fromY + (e.toY - e.fromY) * 0.5},
                     {e.toX}   {e.fromY + (e.toY - e.fromY) * 0.5},
                     {e.toX}   {e.toY}"
                fill="none"
                stroke={e.color}
                stroke-width="2.5"
                opacity="0.9"
              />
            {/if}
          {/each}

          <!-- 2. 节点与极简标签 -->
          {#each renderedNodes as node (node.commit.hash)}
            <!-- svelte-ignore a11y-click-events-have-key-events -->
            <!-- svelte-ignore a11y-no-static-element-interactions -->
            <g class="node-group"
               transform="translate({node.x}, {node.y})"
               on:click={() => selectCommit(node)}>

              <!-- 外层光晕环 -->
              <circle
                r={selectedHash === node.commit.hash ? 14 : (node.isSpine ? 9 : 7)}
                fill={node.color}
                opacity={selectedHash === node.commit.hash ? "0.4" : "0.15"}
                class="glow-ring"
              />

              <!-- 核心彩色节点 (选中时变大) -->
              <circle
                r={selectedHash === node.commit.hash ? 8.5 : 5}
                fill={node.color}
                stroke="#060911"
                stroke-width="2"
                class="core-dot"
              />

              <!-- 点击选中的专属视觉反馈：中心点亮白色小圆点 -->
              {#if selectedHash === node.commit.hash}
                <circle
                  r="3.2"
                  fill="#ffffff"
                />
              {/if}

              <!-- 极简标签：未点击时只显示分支头徽标或半透明7位Hash；点击后就地弹出详情卡片 -->
              <foreignObject x={node.x >= 0 ? 12 : -180} y="-16" width="220" height="70" class="label-box">
                {#if selectedHash === node.commit.hash}
                  <!-- 选中时的详情呼出气泡 -->
                  <div class="popover-card" on:click|stopPropagation>
                    <div class="popover-title">{node.commit.message}</div>
                    <div class="popover-meta">
                      <span>{node.commit.author} · {node.commit.date}</span>
                      <button class="hash-btn" on:click={(e) => copyText(node.commit.hash, e)}>
                        {node.commit.shortHash}
                        <span class="copied-badge">{copiedHash ? '已复制' : '复制'}</span>
                      </button>
                    </div>
                  </div>
                {:else}
                  <!-- 未选中时的默认静默显示：只展示标签/分支头或淡雅Hash -->
                  <div class="silent-label">
                    {#if node.commit.refs && node.commit.refs.length > 0}
                      {#each node.commit.refs as ref}
                        <span class="ref-pill {ref === selectedSpine ? 'spine-pill' : ''}">{ref}</span>
                      {/each}
                    {:else}
                      <span class="faint-hash">{node.commit.shortHash}</span>
                    {/if}
                  </div>
                {/if}
              </foreignObject>
            </g>
          {/each}
        </g>
      </svg>
    </div>
  {/if}
</div>

<style>
  :global(body) {
    margin: 0;
    padding: 0;
    background-color: #060911;
    color: #f1f5f9;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
    user-select: none;
    overflow: hidden;
  }

  .viewport {
    position: relative;
    width: 100vw;
    height: 100vh;
    background: radial-gradient(circle at 50% 30%, #0d1527 0%, #060911 100%);
    overflow: hidden;
    cursor: grab;
    touch-action: none;
  }

  .viewport:active {
    cursor: grabbing;
  }

  /* 顶部浮动控制卡片 */
  .control-panel {
    position: absolute;
    top: 12px;
    left: 50%;
    transform: translateX(-50%);
    max-width: 92vw;
    background: rgba(15, 23, 42, 0.9);
    backdrop-filter: blur(14px);
    border: 1px solid rgba(51, 65, 85, 0.8);
    padding: 5px 12px;
    border-radius: 24px;
    display: flex;
    align-items: center;
    gap: 8px;
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.6);
    z-index: 100;
    box-sizing: border-box;
  }

  .spine-selector-wrap {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-shrink: 1;
    min-width: 0;
  }

  .spine-selector-wrap .label {
    font-size: 11px;
    color: #94a3b8;
    font-weight: 600;
  }

  .spine-select {
    background: #1e293b;
    color: #38bdf8;
    border: 1px solid #334155;
    padding: 2px 6px;
    border-radius: 6px;
    font-size: 11px;
    font-weight: 700;
    outline: none;
    max-width: 110px;
    text-overflow: ellipsis;
  }

  .zoom-tools {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .btn {
    background: #1e293b;
    color: #e2e8f0;
    border: 1px solid #334155;
    width: 24px;
    height: 24px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 13px;
    font-weight: bold;
    cursor: pointer;
  }

  .btn.reset {
    width: auto;
    border-radius: 10px;
    padding: 0 6px;
    font-size: 10px;
  }

  .btn:active {
    background: #38bdf8;
    color: #0f172a;
  }

  .zoom-value {
    font-size: 10px;
    color: #94a3b8;
    min-width: 28px;
    text-align: center;
    font-family: monospace;
  }

  /* 居中画布容器 */
  .canvas-transform {
    position: absolute;
    left: 50%;
    top: 0;
    transform-origin: 50% 0%;
    pointer-events: none;
    will-change: transform;
  }

  .graph-svg {
    display: block;
    pointer-events: auto;
  }

  .spine-center-guide {
    position: absolute;
    left: 450px;
    top: 0;
    bottom: 0;
    width: 1px;
    background: linear-gradient(180deg, rgba(56, 189, 248, 0.22) 0%, rgba(56, 189, 248, 0) 100%);
  }

  .node-group {
    cursor: pointer;
  }

  .core-dot {
    transition: r 0.15s ease-out;
  }

  .label-box {
    overflow: visible;
    pointer-events: auto;
  }

  .silent-label {
    display: flex;
    align-items: center;
    gap: 4px;
    line-height: 32px;
  }

  .faint-hash {
    font-family: monospace;
    font-size: 10px;
    color: #64748b;
    opacity: 0.35;
    letter-spacing: 0.5px;
  }

  .ref-pill {
    background: #0f172a;
    color: #38bdf8;
    border: 1px solid #0284c7;
    font-size: 9px;
    font-weight: 700;
    padding: 1px 5px;
    border-radius: 4px;
    box-shadow: 0 2px 6px rgba(0,0,0,0.4);
    max-width: 120px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .ref-pill.spine-pill {
    background: #0284c7;
    color: #ffffff;
    box-shadow: 0 0 8px rgba(56, 189, 248, 0.5);
  }

  /* 选中时的浮动卡片 */
  .popover-card {
    background: #0f172a;
    border: 1px solid #38bdf8;
    border-radius: 6px;
    padding: 5px 8px;
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.7);
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-width: 190px;
    z-index: 50;
  }

  .popover-title {
    font-size: 11px;
    color: #f8fafc;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .popover-meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-size: 9px;
    color: #94a3b8;
  }

  .hash-btn {
    background: #1e293b;
    color: #38bdf8;
    border: 1px solid #334155;
    font-family: monospace;
    font-size: 9px;
    padding: 1px 4px;
    border-radius: 3px;
    cursor: pointer;
  }

  .copied-badge {
    color: #34d399;
    margin-left: 2px;
  }

  .status-msg {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    color: #64748b;
    font-size: 13px;
  }

  .status-msg.error {
    color: #f87171;
  }
</style>
