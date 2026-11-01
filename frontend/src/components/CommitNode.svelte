<script>
  import { createEventDispatcher } from 'svelte';

  export let node;
  export let selectedSpine = '';
  export let isSelected = false;

  let copiedHash = false;
  const dispatch = createEventDispatcher();

  function onHitboxClick(e) {
    e.stopPropagation();
    dispatch('select', node);
  }

  function copyText(txt, e) {
    e.stopPropagation();
    navigator.clipboard.writeText(txt);
    copiedHash = true;
    setTimeout(() => (copiedHash = false), 1500);
  }

  // 提取纯净的分支名
  function cleanBranchName(ref) {
    if (!ref) return '';
    if (ref.includes('->')) {
      return ref.split('->')[1].trim();
    }
    if (ref.startsWith('origin/HEAD')) {
      return '';
    }
    if (ref.startsWith('origin/')) {
      return ref.replace('origin/', '').trim();
    }
    if (ref.startsWith('tag:')) {
      return '';
    }
    return ref.trim();
  }

  // 判断是否为主轴徽标
  function isSpineRef(ref, currentSpine) {
    if (!currentSpine || !ref) return false;
    const clean = cleanBranchName(ref);
    return clean === currentSpine;
  }

  // 点击分支药丸直接切换为主轴
  function onPillClick(ref, e) {
    e.stopPropagation();
    const branchName = cleanBranchName(ref);
    if (branchName) {
      dispatch('spineSelect', branchName);
    }
  }
</script>

<g class="node-group" transform="translate({node.x}, {node.y})">
  <!-- 外发光环 -->
  <circle
    r={isSelected ? 14 : (node.isSpine ? 9 : 7)}
    fill={node.color}
    opacity={isSelected ? "0.4" : "0.15"}
    style="pointer-events: none;"
  />

  <!-- 核心彩色节点 -->
  <circle
    r={isSelected ? 8.5 : 5}
    fill={node.color}
    stroke="#060911"
    stroke-width="2"
    style="pointer-events: none;"
  />

  <!-- 选中白芯 -->
  {#if isSelected}
    <circle r="3.2" fill="#ffffff" style="pointer-events: none;" />
  {/if}

  <!-- 精准触控垫 (14px) -->
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <circle
    r="14"
    fill="transparent"
    class="touch-hitbox"
    on:click={onHitboxClick}
  />

  <!-- 标签排版盒子 -->
  <foreignObject
    x={node.isLeft ? -222 : 12}
    y="-14"
    width="210"
    height="80"
    class="label-box {isSelected ? 'active-events' : 'passthrough-events'}"
  >
    {#if isSelected}
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="popover-card {node.isLeft ? 'on-left' : 'on-right'}" on:click|stopPropagation>
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
      <div class="stacked-label {node.isLeft ? 'align-right' : 'align-left'}">
        <span class="faint-hash">{node.commit.shortHash}</span>
        {#if node.commit.refs && node.commit.refs.length > 0}
          <div class="pills-strip {node.isLeft ? 'justify-right' : 'justify-left'}">
            {#each node.commit.refs as ref}
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <span
                class="ref-pill {isSpineRef(ref, selectedSpine) ? 'spine-pill' : ''}"
                on:click={(e) => onPillClick(ref, e)}
                title="点击将此分支拉直为主轴"
              >
                {ref}
              </span>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  </foreignObject>
</g>

<style>
  .touch-hitbox {
    cursor: pointer;
    touch-action: manipulation;
  }

  .label-box {
    overflow: visible;
  }

  .label-box.passthrough-events {
    pointer-events: none;
  }

  .label-box.active-events {
    pointer-events: auto;
  }

  .stacked-label {
    display: flex;
    flex-direction: column;
    gap: 2px;
    width: 100%;
  }

  .stacked-label.align-left {
    align-items: flex-start;
  }

  .stacked-label.align-right {
    align-items: flex-end;
  }

  .faint-hash {
    font-family: monospace;
    font-size: 10px;
    color: #94a3b8;
    opacity: 0.45;
    letter-spacing: 0.5px;
    line-height: 12px;
  }

  .pills-strip {
    display: flex;
    flex-wrap: wrap;
    gap: 3px;
  }

  .pills-strip.justify-left {
    justify-content: flex-start;
  }

  .pills-strip.justify-right {
    justify-content: flex-end;
  }

  /* 药丸徽标支持独立直接点击 */
  .ref-pill {
    pointer-events: auto; /* 覆盖父级的 none，确保药丸可点击 */
    background: #0f172a;
    color: #38bdf8;
    border: 1px solid #0284c7;
    font-size: 8.5px;
    font-weight: 700;
    padding: 0px 5px;
    height: 15px;
    line-height: 14px;
    border-radius: 3px;
    box-shadow: 0 1px 4px rgba(0,0,0,0.4);
    max-width: 120px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    display: inline-block;
    box-sizing: border-box;
    cursor: pointer;
    transition: transform 0.1s, box-shadow 0.1s;
  }

  .ref-pill:active {
    transform: scale(0.95);
  }

  .ref-pill.spine-pill {
    background: #0284c7;
    color: #ffffff;
    box-shadow: 0 0 8px rgba(56, 189, 248, 0.6);
    border-color: #38bdf8;
  }

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

  .popover-card.on-right {
    margin-right: auto;
  }

  .popover-card.on-left {
    margin-left: auto;
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
</style>
