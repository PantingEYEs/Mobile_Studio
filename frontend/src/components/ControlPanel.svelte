<script>
  import { createEventDispatcher } from 'svelte';

  export let zoom = 1.0;
  const dispatch = createEventDispatcher();

  function onReset() {
    dispatch('resetView');
  }
</script>

<div class="control-panel" on:mousedown|stopPropagation on:touchstart|stopPropagation>
  <div class="status-tools">
    <span class="zoom-value">{Math.round(zoom * 100)}%</span>
    <button class="btn reset" on:click={onReset}>居中</button>
  </div>
</div>

<style>
  .control-panel {
    position: absolute;
    top: 14px;
    left: 50%;
    transform: translateX(-50%);
    background: rgba(15, 23, 42, 0.88);
    backdrop-filter: blur(14px);
    border: 1px solid rgba(51, 65, 85, 0.75);
    padding: 3px 8px;
    border-radius: 18px;
    display: flex;
    align-items: center;
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.5);
    z-index: 100;
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
