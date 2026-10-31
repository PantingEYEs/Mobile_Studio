export const SPINE_COLOR = '#38bdf8';
export const BRANCH_COLORS = [
  '#34d399', // 薄荷绿
  '#f472b6', // 珊瑚粉
  '#fbbf24', // 琥珀黄
  '#a78bfa', // 紫罗兰
  '#fb7185'  // 玫瑰红
];

export const ROW_GAP = 52;
export const LANE_SPREAD = 22;

export function layoutTopology(commitList, spineBranchName) {
  if (!commitList || commitList.length === 0) {
    return { nodes: [], edges: [] };
  }

  const commitMap = new Map();
  commitList.forEach(c => commitMap.set(c.hash, c));
  const rowMap = new Map();
  commitList.forEach((c, idx) => rowMap.set(c.hash, idx));

  // 1. 定位主轴 Head 并溯源拉直主干
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
    cur = (node.parents && node.parents.length > 0) ? node.parents[0] : null;
  }

  // 2. 提取所有非主干分支链 (Branch Chains) 及其生存区间 [startRow, endRow]
  const visited = new Set();
  const chains = [];

  for (let i = 0; i < commitList.length; i++) {
    const c = commitList[i];
    if (spineCommitHashes.has(c.hash) || visited.has(c.hash)) continue;

    const chainCommits = [];
    let w = c.hash;
    while (w && !spineCommitHashes.has(w) && commitMap.has(w)) {
      if (visited.has(w)) break;
      visited.add(w);
      chainCommits.push(w);
      const p = commitMap.get(w).parents;
      w = (p && p.length > 0) ? p[0] : null;
    }

    const forkBase = w;
    const headHash = chainCommits[0];

    // 寻找合并该分支链最高处的父提交行号 (确定活跃生存区间起点)
    let mergeRow = rowMap.get(headHash);
    for (let r = 0; r < commitList.length; r++) {
      const cand = commitList[r];
      if (cand.parents && cand.parents.length > 1 && cand.parents[1] === headHash) {
        mergeRow = r;
        break;
      }
    }

    const endRow = forkBase && rowMap.has(forkBase) ? rowMap.get(forkBase) : commitList.length - 1;

    chains.push({
      commits: chainCommits,
      head: headHash,
      forkBase: forkBase,
      startRow: Math.min(mergeRow, rowMap.get(headHash)),
      endRow: endRow
    });
  }

  // 3. 区间图复用与分叉步进泳道分配 (统一规则：无冲突即复用，同源冲突自动基于前序分支向外步进)
  const laneOccupancy = new Map(); // laneNumber -> Set of occupied row indices
  function isLaneFree(lane, startR, endR) {
    if (!laneOccupancy.has(lane)) return true;
    const occ = laneOccupancy.get(lane);
    for (let r = startR; r <= endR; r++) {
      if (occ.has(r)) return false;
    }
    return true;
  }
  function occupyLane(lane, startR, endR) {
    if (!laneOccupancy.has(lane)) laneOccupancy.set(lane, new Set());
    const occ = laneOccupancy.get(lane);
    for (let r = startR; r <= endR; r++) {
      occ.add(r);
    }
  }

  // 主轴始终占满整个生命周期的 Lane 0
  occupyLane(0, 0, commitList.length - 1);

  const chainLanes = new Map();
  // 记录每个 Base Commit 处当前分支的相对步进，天然衍生多分支递增
  const forkBaseOffsets = new Map();

  chains.forEach((ch, idx) => {
    const baseHash = ch.forkBase;
    let baseLane = 0;
    if (baseHash && chainLanes.has(baseHash)) {
      baseLane = chainLanes.get(baseHash);
    }

    let chosenLane = null;

    if (baseLane === 0) {
      // 从主干分叉出的顶层分支：
      let side = (idx % 2 === 0) ? 1 : -1;
      let startStep = 1;

      // 若相同 Commit 上已有早先分支，天然以此分支为基准递增一步 (+1)
      if (baseHash && forkBaseOffsets.has(baseHash)) {
        const prev = forkBaseOffsets.get(baseHash);
        side = prev.side;
        startStep = prev.step + 1;
      }

      let step = startStep;
      while (chosenLane === null) {
        const l = side * step;
        if (isLaneFree(l, ch.startRow, ch.endRow)) {
          chosenLane = l;
          forkBaseOffsets.set(baseHash, { side, step });
          break;
        }
        step++;
      }
    } else {
      // 从子分支再分叉出的孙分支：严格锁定在父分支同侧，以父分支列号为基准向外递增 (+1)
      const sign = baseLane > 0 ? 1 : -1;
      let offset = 1;
      while (chosenLane === null) {
        const l = baseLane + sign * offset;
        if (isLaneFree(l, ch.startRow, ch.endRow)) {
          chosenLane = l;
          break;
        }
        offset++;
      }
    }

    occupyLane(chosenLane, ch.startRow, ch.endRow);
    ch.commits.forEach(h => chainLanes.set(h, chosenLane));
  });

  // 4. 计算每个提交节点的物理坐标 (X, Y)
  const nodeCoords = new Map();
  const nodes = commitList.map((c, index) => {
    const isSpine = spineCommitHashes.has(c.hash);
    let laneIndex = 0;
    let color = SPINE_COLOR;

    if (!isSpine) {
      laneIndex = chainLanes.get(c.hash) || 1;
      color = BRANCH_COLORS[(Math.abs(laneIndex) - 1) % BRANCH_COLORS.length];
    }

    const x = laneIndex * LANE_SPREAD;
    const y = index * ROW_GAP;
    const isLeft = laneIndex < 0;
    nodeCoords.set(c.hash, { x, y, color, isSpine });

    return { commit: c, x, y, color, isSpine, isLeft };
  });

  // 5. 提取真实 DAG 有向边
  const edges = [];
  commitList.forEach(c => {
    const from = nodeCoords.get(c.hash);
    if (!from || !c.parents) return;

    c.parents.forEach((pHash, pIndex) => {
      const to = nodeCoords.get(pHash);
      if (!to) return;

      const edgeColor = from.isSpine && to.isSpine && pIndex === 0 ? SPINE_COLOR : from.color;
      edges.push({
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

  return { nodes, edges };
}
