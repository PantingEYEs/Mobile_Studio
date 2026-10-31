<script>
  import { createEventDispatcher } from 'svelte';

  export let branches = [];
  export let selectedSpine = '';
  export let zoom = 1.0;

  const dispatch = createEventDispatcher();

  function onSpineChange(e) {
    dispatch('spineChange', e.target.value);
  }

  function onReset() {
    dispatch('resetView');
  }
</script>

<div class="control-panel" on:mousedown|stopPropagation on:touchstart|stopPropagation>
  <div class="spine-selector-wrap">
    <span class="label">主轴:</span>
    <select class="spine-select" value={selectedSpine} on:change={onSpineChange}>
      {#each branches as b}
        <option value={b}>{b}</option>
      {/each}
    </select>
  </div>

  <div class="status-tools">
    <span class="zoom-value">{Math.round(zoom * 100)}%</span>
    <button class="btn reset" on:click={onReset}>居中</button>
  </div>
</div>

<style>
  .control-panel {
    position: absolute;
    top: 12px;
    left: 50%;
    transform: translateX(-50%);
    max-width: 90vw;
    background: rgba(15, 23, 42, 0.9);
    backdrop-filter: blur(14px);
    border: 1px solid rgba(51, 65, 85, 0.8);
    padding: 4px 10px;
    border-radius: 20px;
    display: flex;
    align-items: center;
    gap: 8px;
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.6);
    z-index: 100;
  }

  .spine-selector-wrap {
    display: flex;
    align-items: center;
    gap: 4px;
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

  .status-tools {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .zoom-value {
    font-size: 10px;
    color: #94a3b8;
    min-width: 28px;
    text-align: center;
    font-family: monospace;
  }

  .btn.reset {
    background: #1e293b;
    color: #e2e8f0;
    border: 1px solid #334155;
    border-radius: 10px;
    padding: 2px 8px;
    font-size: 10px;
    font-weight: bold;
    cursor: pointer;
  }

  .btn.reset:active {
    background: #38bdf8;
    color: #0f172a;
  }
</style>
