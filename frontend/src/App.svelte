<script>
  import { onMount } from 'svelte';
  import { layoutTopology, ROW_GAP } from './lib/graphLayout.js';
  import ControlPanel from './components/ControlPanel.svelte';
  import CommitNode from './components/CommitNode.svelte';
  import FilePanel from './components/FilePanel.svelte';

  let branches = [];
  let selectedSpine = '';
  let commits = [];
  let loading = true;
  let error = null;

  let selectedHash = null;

  // 手势平移与双指缩放
  let zoom = 1.0;
  let offsetX = 0;
  let offsetY = 80;

  let isDragging = false;
  let hasMoved = false; // 标记本次手势是否发生了真实移动
  let startX = 0, startY = 0;
  let startOffsetX = 0, startOffsetY = 0;

  let isPinching = false;
  let initialPinchDistance = 0;
  let initialZoom = 1.0;

  let renderedNodes = [];
  let renderedEdges = [];

  function getDistance(t) {
    return Math.hypot(t[0].clientX - t[1].clientX, t[0].clientY - t[1].clientY);
  }

  function refreshLayout() {
    const res = layoutTopology(commits, selectedSpine);
    renderedNodes = res.nodes;
    renderedEdges = res.edges;
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
        selectedSpine = branches.includes('0.0.1-dev') ? '0.0.1-dev' : (data.currentBranch || branches[0] || '');
      }
      refreshLayout();
    } catch (e) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  function handleSpineChange(e) {
    selectedSpine = e.detail;
    refreshLayout();
  }

  // 严格校验：若刚才发生了滑动平移，坚决不触发节点点击选中！
  function handleSelectNode(e) {
    if (hasMoved) return;
    const node = e.detail;
    selectedHash = selectedHash === node.commit.hash ? null : node.commit.hash;
  }

  function resetView() {
    zoom = 1.0;
    offsetX = 0;
    offsetY = 80;
  }

  function onTouchStart(e) {
    hasMoved = false;
    if (e.touches && e.touches.length === 2) {
      isPinching = true;
      isDragging = false;
      initialPinchDistance = getDistance(e.touches);
      initialZoom = zoom;
    } else if (e.touches && e.touches.length === 1) {
      isPinching = false;
      isDragging = true;
      startX = e.touches[0].clientX;
      startY = e.touches[0].clientY;
      startOffsetX = offsetX;
      startOffsetY = offsetY;
    } else if (!e.touches) {
      isDragging = true;
      startX = e.clientX;
      startY = e.clientY;
      startOffsetX = offsetX;
      startOffsetY = offsetY;
    }
  }

  function onTouchMove(e) {
    if (isPinching && e.touches && e.touches.length === 2) {
      const dist = getDistance(e.touches);
      if (initialPinchDistance > 0) {
        zoom = Math.min(1.8, Math.max(0.5, +(initialZoom * (dist / initialPinchDistance)).toFixed(2)));
      }
    } else if (isDragging) {
      const cx = e.touches ? e.touches[0].clientX : e.clientX;
      const cy = e.touches ? e.touches[0].clientY : e.clientY;
      const dx = cx - startX;
      const dy = cy - startY;

      // 位移超过 6px 判定为滑动操作
      if (Math.hypot(dx, dy) > 6) {
        hasMoved = true;
      }

      offsetX = startOffsetX + dx;
      offsetY = startOffsetY + dy;
    }
  }

  function onTouchEnd(e) {
    if (e.touches && e.touches.length < 2) isPinching = false;
    if (!e.touches || e.touches.length === 0) {
      isDragging = false;
      isPinching = false;
      // 延时重置 hasMoved，避免 click 事件冒泡拾取到过期的未移动标志
      setTimeout(() => {
        hasMoved = false;
      }, 50);
    }
  }

  function onWheel(e) {
    e.preventDefault();
    zoom = Math.min(1.8, Math.max(0.5, +(zoom - e.deltaY * 0.001).toFixed(2)));
  }

  onMount(() => loadData());
</script>

<div class="viewport"
     on:mousedown={onTouchStart} on:mousemove={onTouchMove} on:mouseup={onTouchEnd}
     on:touchstart={onTouchStart} on:touchmove={onTouchMove} on:touchend={onTouchEnd}
     on:wheel={onWheel}>

  <FilePanel isRoot={true} />

  <ControlPanel
    {branches}
    {selectedSpine}
    {zoom}
    on:spineChange={handleSpineChange}
    on:resetView={resetView}
  />

  {#if loading}
    <div class="status-msg">正在梳理拓扑树...</div>
  {:else if error}
    <div class="status-msg error">{error}</div>
  {:else}
    <div class="canvas-transform"
         style="transform: translate(calc(-50% + {offsetX}px), {offsetY}px) scale({zoom});">

      <div class="spine-center-guide"></div>

      <svg class="graph-svg" width="900" height={commits.length * ROW_GAP + 160}>
        <g transform="translate(450, 40)">
          {#each renderedEdges as e (e.id)}
            {#if e.isStraight}
              <line x1={e.fromX} y1={e.fromY} x2={e.toX} y2={e.toY} stroke={e.color} stroke-width="2.6" opacity="0.85" />
            {:else}
              <path
                d="M {e.fromX} {e.fromY} C {e.fromX} {e.fromY + (e.toY - e.fromY) * 0.5}, {e.toX} {e.fromY + (e.toY - e.fromY) * 0.5}, {e.toX} {e.toY}"
                fill="none" stroke={e.color} stroke-width="2.5" opacity="0.9"
              />
            {/if}
          {/each}

          {#each renderedNodes as node (node.commit.hash)}
            <CommitNode
              {node}
              {selectedSpine}
              isSelected={selectedHash === node.commit.hash}
              on:select={handleSelectNode}
            />
          {/each}
        </g>
      </svg>
    </div>
  {/if}
</div>

<style>
  :global(body) {
    margin: 0; padding: 0;
    background-color: #060911; color: #f1f5f9;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
    user-select: none; overflow: hidden;
  }

  .viewport {
    position: relative; width: 100vw; height: 100vh;
    background: radial-gradient(circle at 50% 30%, #0d1527 0%, #060911 100%);
    overflow: hidden; cursor: grab; touch-action: none;
  }
  .viewport:active { cursor: grabbing; }

  .canvas-transform {
    position: absolute; left: 50%; top: 0;
    transform-origin: 50% 0%; pointer-events: none; will-change: transform;
  }

  .graph-svg { display: block; pointer-events: auto; }

  .spine-center-guide {
    position: absolute; left: 450px; top: 0; bottom: 0; width: 1px;
    background: linear-gradient(180deg, rgba(56, 189, 248, 0.22) 0%, rgba(56, 189, 248, 0) 100%);
  }

  .status-msg {
    position: absolute; top: 50%; left: 50%;
    transform: translate(-50%, -50%); color: #64748b; font-size: 13px;
  }
  .status-msg.error { color: #f87171; }
</style>
