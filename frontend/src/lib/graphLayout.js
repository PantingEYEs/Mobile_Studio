// 经典多色系调色盘 (主轴专属天蓝，分支轮替绚丽色彩)
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

/**
 * 核心拓扑排版算法：输入提交列表与选定主轴名称，输出物理坐标与精确有向边
 */
export function layoutTopology(commitList, spineBranchName) {
  if (!commitList || commitList.length === 0) {
    return { nodes: [], edges: [] };
  }

  const commitMap = new Map();
  commitList.forEach(c => commitMap.set(c.hash, c));

  // 1. 定位主轴 Head 并沿 first-parent 溯源锁定主干链
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

  // 2. 识别分支链 (Chain)：同一分支的所有 Commit 共享同一分支根
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

  // 3. 为每个分支分配稳定的侧翼泳道 (绝不跨侧横跳)
  const chainLaneMap = new Map();
  let nextRightLane = 1;
  let nextLeftLane = -1;
  let colorIdx = 0;

  commitList.forEach(c => {
    if (spineCommitHashes.has(c.hash)) return;
    const root = chainRootMap.get(c.hash) || c.hash;
    if (!chainLaneMap.has(root)) {
      let lane = (Math.abs(nextLeftLane) <= nextRightLane) ? nextLeftLane-- : nextRightLane++;
      const color = BRANCH_COLORS[colorIdx % BRANCH_COLORS.length];
      colorIdx++;
      chainLaneMap.set(root, { lane, color });
    }
  });

  // 4. 计算每个提交节点的物理坐标 (X, Y)
  const nodeCoords = new Map();
  const nodes = commitList.map((c, index) => {
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
