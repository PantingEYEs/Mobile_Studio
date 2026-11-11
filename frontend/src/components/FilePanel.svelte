<script>
  import { onMount } from 'svelte';

  export let node = null;
  export let selectedPath = null;
  export let isRoot = false;

  export let expandedDirs = {};
  export let loadingDirs = {};

  export let currentProject = null;
  export let projects = [];

  export let openCreateModal = () => {};
  export let openRenameModal = () => {};

  let isPanelOpen = false;
  let isProjDrawerOpen = false;
  let rootTree = null;

  // 模态框状态
  let showNewProjModal = false;
  let newProjName = '';
  let newProjDir = '~';
  let newProjType = 'blank';
  let newProjPkg = '';
  let newProjLang = 'kotlin';
  let newProjMinSdk = '24';
  let newProjTargetSdk = '35';
  let newProjCompileSdk = '35';
  let newProjTemplate = 'empty_views';

  $: defaultPkg = 'com.example.' + (newProjName.trim() ? newProjName.trim().toLowerCase().replace(/[^a-z0-9_]/g, '_') : 'myapp');

  let showImportProjModal = false;
  let importMode = 'local'; // "local" or "clone"
  let importLocalPath = '~';
  let importGitURL = '';
  let importGitDir = '~';

  let showDeleteModal = false;
  let projectToDelete = null;

  let showCreateModal = false;
  let createTargetNode = null;
  let createItemType = 'file';
  let createItemName = '';

  let showRenameModal = false;
  let renameTargetNode = null;
  let renameNewName = '';

  let isSubmitting = false;

  // 文件夹选择器状态
  let showFolderPicker = false;
  let pickerTarget = ''; // 'newProjDir' | 'importLocalPath' | 'importGitDir'
  let pickerCurrent = '~';
  let pickerParent = '';
  let pickerDirs = [];
  let isPickerLoading = false;

  async function openFolderPicker(target, initialDir) {
    pickerTarget = target;
    showFolderPicker = true;
    await browseFolder(initialDir || '~');
  }

  async function browseFolder(dirPath) {
    isPickerLoading = true;
    try {
      const res = await fetch(`/api/fs/browse?dir=${encodeURIComponent(dirPath)}`);
      if (res.ok) {
        const data = await res.json();
        pickerCurrent = data.current || dirPath;
        pickerParent = data.parent || '';
        pickerDirs = data.dirs || [];
      }
    } catch (e) {
      console.error('Failed to browse dir:', e);
    } finally {
      isPickerLoading = false;
    }
  }

  function confirmFolderPicker() {
    if (pickerTarget === 'newProjDir') newProjDir = pickerCurrent;
    else if (pickerTarget === 'importLocalPath') importLocalPath = pickerCurrent;
    else if (pickerTarget === 'importGitDir') importGitDir = pickerCurrent;
    showFolderPicker = false;
  }

  let longPressTimer = null;
  let touchStartX = 0;
  let touchStartY = 0;

  const IGNORED_NAMES = new Set(['.git', 'node_modules', 'bin', 'dist', '.idea', '.vscode', '.svelte-kit']);

  const SELF_PROJECT = { name: 'Mobile_Studio', path: '.' };

  // 项目列表保存在服务端注册表（~/.mobile_studio/projects.json），与 m_s project 命令共享
  async function loadProjects() {
    let list = projects;
    try {
      // 一次性迁移：旧版本保存在浏览器 localStorage 中的项目登记到服务端
      const legacy = JSON.parse(localStorage.getItem('ms_projects') || 'null');
      if (Array.isArray(legacy)) {
        for (const p of legacy) {
          if (p && p.path && p.path !== '.') {
            await fetch(`/api/projects/add?path=${encodeURIComponent(p.path)}&name=${encodeURIComponent(p.name || '')}`).catch(() => {});
          }
        }
        localStorage.removeItem('ms_projects');
      }
    } catch (e) {}
    try {
      const res = await fetch('/api/projects');
      const data = await res.json();
      if (data.success) list = data.projects || [];
    } catch (e) {
      console.error('Failed to load projects:', e);
    }
    projects = list;
    return list;
  }

  function rememberCurrent(p) {
    try {
      localStorage.setItem('ms_current_project', p.path);
    } catch (e) {}
  }

  function pickInitialProject() {
    let saved = null;
    try {
      saved = localStorage.getItem('ms_current_project');
    } catch (e) {}
    return projects.find((p) => p.path === saved) || projects[0] || SELF_PROJECT;
  }

  async function toggleProjDrawer() {
    isProjDrawerOpen = !isProjDrawerOpen;
    if (isProjDrawerOpen) await loadProjects();
  }

  // 新建 / 导入 / 克隆成功后：刷新列表并切换到新项目（服务端已自动登记）
  async function openRegisteredProject(data) {
    await loadProjects();
    switchProject(projects.find((p) => p.path === data.path) || { name: data.name, path: data.path });
  }

  async function fetchChildren(dirPath) {
    try {
      const rootParam = currentProject ? currentProject.path : '.';
      const url = `/api/fs/tree?root=${encodeURIComponent(rootParam)}&path=${encodeURIComponent(dirPath)}`;
      const res = await fetch(url);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      return data.children || [];
    } catch (e) {
      console.error('Failed to fetch layer:', dirPath, e);
      return [];
    }
  }

  async function loadRootTree() {
    if (!currentProject) return;
    const children = await fetchChildren('');
    rootTree = {
      name: currentProject.name,
      path: '',
      isDir: true,
      isProjectRoot: true,
      children: children
    };
    expandedDirs[''] = true;
    expandedDirs = { ...expandedDirs };
  }

  function togglePanel() {
    isPanelOpen = !isPanelOpen;
    if (isPanelOpen && !rootTree) {
      loadRootTree();
    }
  }

  function switchProject(p) {
    currentProject = p;
    rememberCurrent(p);
    isProjDrawerOpen = false;
    expandedDirs = {};
    loadingDirs = {};
    selectedPath = null;
    loadRootTree();
  }

  function confirmDeleteProject(p, e) {
    e.stopPropagation();
    projectToDelete = p;
    showDeleteModal = true;
  }

  async function executeDeleteProject() {
    if (!projectToDelete) return;
    const target = projectToDelete;
    showDeleteModal = false;
    projectToDelete = null;
    try {
      const res = await fetch(`/api/projects/remove?path=${encodeURIComponent(target.path)}`);
      const data = await res.json();
      if (!data.success) alert('移除失败: ' + (data.error || '未知错误'));
    } catch (e) {
      alert('移除失败: ' + e.message);
    }
    await loadProjects();
    if (currentProject.path === target.path) {
      switchProject(projects[0] || SELF_PROJECT);
    }
  }

  async function executeCreateProject() {
    if (!newProjName.trim()) return;
    if (newProjType === 'android' && (+newProjMinSdk > +newProjTargetSdk || +newProjTargetSdk > +newProjCompileSdk)) return;
    isSubmitting = true;
    try {
      const pkgParam = (newProjPkg.trim() || defaultPkg);
      const url = `/api/fs/project/create?dir=${encodeURIComponent(newProjDir)}&name=${encodeURIComponent(newProjName.trim())}&type=${newProjType}&pkg=${encodeURIComponent(pkgParam)}&lang=${encodeURIComponent(newProjLang)}&minSdk=${encodeURIComponent(newProjMinSdk)}&targetSdk=${encodeURIComponent(newProjTargetSdk)}&compileSdk=${encodeURIComponent(newProjCompileSdk)}&template=${encodeURIComponent(newProjTemplate)}`;
      const res = await fetch(url);
      const data = await res.json();
      if (data.success) {
        showNewProjModal = false;
        newProjName = '';
        await openRegisteredProject(data);
      } else {
        alert('创建失败: ' + (data.error || '未知错误'));
      }
    } catch (e) {
      alert('网络请求失败: ' + e.message);
    } finally {
      isSubmitting = false;
    }
  }

  // 导入项目执行逻辑 (本地 / Git Clone)
  async function executeImportProject() {
    isSubmitting = true;
    try {
      if (importMode === 'local') {
        if (!importLocalPath.trim()) return;
        const url = `/api/fs/project/import?path=${encodeURIComponent(importLocalPath.trim())}`;
        const res = await fetch(url);
        const data = await res.json();
        if (data.success) {
          showImportProjModal = false;
          await openRegisteredProject(data);
        } else {
          alert('导入失败: ' + (data.error || '路径无效'));
        }
      } else {
        if (!importGitURL.trim()) return;
        const url = `/api/fs/project/clone?url=${encodeURIComponent(importGitURL.trim())}&dir=${encodeURIComponent(importGitDir.trim())}`;
        const res = await fetch(url);
        const data = await res.json();
        if (data.success) {
          showImportProjModal = false;
          importGitURL = '';
          await openRegisteredProject(data);
        } else {
          alert('克隆失败: ' + (data.error || '网络或仓库地址错误'));
        }
      }
    } catch (e) {
      alert('导入失败: ' + e.message);
    } finally {
      isSubmitting = false;
    }
  }

  async function toggleDir(dirNode, e) {
    e.stopPropagation();
    const p = dirNode.path;

    if (expandedDirs[p]) {
      expandedDirs[p] = false;
      expandedDirs = { ...expandedDirs };
      return;
    }

    if (!dirNode.children) {
      loadingDirs[p] = true;
      loadingDirs = { ...loadingDirs };
      dirNode.children = await fetchChildren(p);
      loadingDirs[p] = false;
      loadingDirs = { ...loadingDirs };
    }

    expandedDirs[p] = true;
    expandedDirs = { ...expandedDirs };
  }

  async function executeCreateItem() {
    if (!createItemName.trim()) return;
    isSubmitting = true;
    try {
      const targetDir = createTargetNode.path;
      const childRelPath = targetDir ? `${targetDir}/${createItemName.trim()}` : createItemName.trim();
      const rootParam = currentProject ? currentProject.path : '.';
      const url = `/api/fs/create?root=${encodeURIComponent(rootParam)}&path=${encodeURIComponent(childRelPath)}&type=${createItemType}`;
      const res = await fetch(url);
      const data = await res.json();
      if (data.success) {
        createTargetNode.children = await fetchChildren(createTargetNode.path);
        expandedDirs[createTargetNode.path] = true;
        expandedDirs = { ...expandedDirs };
        showCreateModal = false;
        createItemName = '';
      }
    } catch (e) {
      alert('创建失败: ' + e.message);
    } finally {
      isSubmitting = false;
    }
  }

  async function executeRenameItem() {
    if (!renameNewName.trim()) return;
    isSubmitting = true;
    try {
      const rootParam = currentProject ? currentProject.path : '.';
      const url = `/api/fs/rename?root=${encodeURIComponent(rootParam)}&oldPath=${encodeURIComponent(renameTargetNode.path)}&newName=${encodeURIComponent(renameNewName.trim())}`;
      const res = await fetch(url);
      const data = await res.json();
      if (data.success) {
        loadRootTree();
        showRenameModal = false;
        renameNewName = '';
      }
    } catch (e) {
      alert('重命名失败: ' + e.message);
    } finally {
      isSubmitting = false;
    }
  }

  function handleTouchStart(itemNode, e) {
    if (e.touches && e.touches.length === 1) {
      touchStartX = e.touches[0].clientX;
      touchStartY = e.touches[0].clientY;
      clearTimeout(longPressTimer);
      longPressTimer = setTimeout(() => {
        openRenameModal(itemNode);
      }, 550);
    }
  }

  function handleTouchMove(e) {
    if (e.touches && e.touches.length === 1) {
      const dx = Math.abs(e.touches[0].clientX - touchStartX);
      const dy = Math.abs(e.touches[0].clientY - touchStartY);
      if (dx > 8 || dy > 8) {
        clearTimeout(longPressTimer);
      }
    }
  }

  function handleTouchEnd() {
    clearTimeout(longPressTimer);
  }

  onMount(async () => {
    if (isRoot) {
      await loadProjects();
      if (!currentProject) currentProject = pickInitialProject();
      loadRootTree();
    }
  });
</script>

{#if isRoot}
  <button class="panel-trigger-btn {isPanelOpen ? 'active' : ''}" on:click={togglePanel}>
    <span class="icon">📁</span>
    <span class="text">文件</span>
  </button>

  {#if isPanelOpen}
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div class="panel-backdrop" on:click={togglePanel}></div>
    <div class="file-panel" on:click|stopPropagation>
      
      <div class="panel-header">
        <!-- svelte-ignore a11y-click-events-have-key-events -->
        <!-- svelte-ignore a11y-no-static-element-interactions -->
        <div class="header-title project-switch-btn" on:click={toggleProjDrawer}>
          <span class="proj-icon">📦</span>
          <span class="proj-name">{currentProject ? currentProject.name : '项目选择'}</span>
          <span class="switch-arrow">{isProjDrawerOpen ? '▲' : '▼'}</span>
        </div>
        <div class="header-actions">
          <button class="icon-btn" on:click={loadRootTree} title="刷新目录">🔄</button>
          <button class="icon-btn close" on:click={togglePanel}>✕</button>
        </div>
      </div>

      {#if isProjDrawerOpen}
        <div class="project-drawer">
          <div class="drawer-section-title">已有项目</div>
          <div class="project-list">
            {#each projects as p}
              <div class="project-item {currentProject.path === p.path ? 'active' : ''} {p.exists === false ? 'missing' : ''}" on:click={() => switchProject(p)}>
                <div class="proj-info">
                  <span class="p-name">{p.name}{#if p.exists === false}<span class="p-missing">（目录不存在）</span>{/if}</span>
                  <span class="p-path">{p.path}</span>
                </div>
                <button class="del-proj-btn" on:click={(e) => confirmDeleteProject(p, e)} title="从列表中清除">🗑️</button>
              </div>
            {/each}
          </div>
          <div class="project-actions">
            <button class="act-btn create" on:click={() => (showNewProjModal = true)}>
              <span>➕</span> 新建项目
            </button>
            <button class="act-btn import" on:click={() => (showImportProjModal = true)}>
              <span>📥</span> 导入项目
            </button>
          </div>
        </div>
      {/if}

      <div class="tree-container">
        {#if rootTree}
          <svelte:self
            node={rootTree}
            bind:selectedPath
            bind:expandedDirs
            bind:loadingDirs
            {currentProject}
            openCreateModal={(target) => {
              createTargetNode = target;
              createItemType = 'file';
              createItemName = '';
              showCreateModal = true;
            }}
            openRenameModal={(target) => {
              renameTargetNode = target;
              renameNewName = target.name;
              showRenameModal = true;
            }}
            isRoot={false}
          />
        {:else}
          <div class="loading-hint">正在读取文件树...</div>
        {/if}
      </div>
    </div>
  {/if}

  <!-- 模态框 1：新建项目向导 -->
  {#if showNewProjModal}
    <div class="modal-backdrop">
      <div class="modal-box">
        <div class="modal-title">新建项目</div>
        <div class="form-item">
          <label>项目名称</label>
          <input type="text" bind:value={newProjName} placeholder="如：MyAwesomeApp" />
        </div>
        <div class="form-item">
          <label>存放目录 (默认家目录)</label>
          <div class="input-with-btn"><input type="text" bind:value={newProjDir} placeholder="~" /><button type="button" class="browse-btn" on:click={() => openFolderPicker('newProjDir', newProjDir)}>📂 浏览</button></div>
        </div>
        <div class="form-item">
          <label>项目类型</label>
          <div class="radio-group">
            <label class="radio-label">
              <input type="radio" bind:group={newProjType} value="blank" /> 空白项目
            </label>
            <label class="radio-label">
              <input type="radio" bind:group={newProjType} value="android" /> Android 项目
            </label>
          </div>
        </div>

        {#if newProjType === 'android'}
          <div class="android-config-panel">
            <div class="form-item">
              <label>应用包名 (Package Name)</label>
              <input type="text" bind:value={newProjPkg} placeholder={defaultPkg} />
            </div>
            <div class="form-item">
              <label>开发语言 (Language)</label>
              <div class="radio-group">
                <label class="radio-label">
                  <input type="radio" bind:group={newProjLang} value="kotlin" /> Kotlin
                </label>
                <label class="radio-label">
                  <input type="radio" bind:group={newProjLang} value="java" /> Java
                </label>
              </div>
            </div>
            <div class="sdk-row">
              <div class="form-item half">
                <label>最低 SDK (Min)</label>
                <select bind:value={newProjMinSdk}>
                  <option value="24">API 24 (Android 7.0，推荐)</option>
                  <option value="26">API 26 (Android 8.0)</option>
                  <option value="29">API 29 (Android 10)</option>
                  <option value="31">API 31 (Android 12)</option>
                  <option value="33">API 33 (Android 13)</option>
                  <option value="34">API 34 (Android 14)</option>
                  <option value="35">API 35 (Android 15)</option>
                </select>
              </div>
              <div class="form-item half">
                <label>目标 SDK (Target)</label>
                <select bind:value={newProjTargetSdk}>
                  <option value="33">API 33 (Android 13)</option>
                  <option value="34">API 34 (Android 14)</option>
                  <option value="35">API 35 (Android 15)</option>
                </select>
              </div>
            </div>
            <div class="form-item">
              <label>编译 SDK (Compile)</label>
              <select bind:value={newProjCompileSdk}>
                <option value="33">API 33 (Android 13)</option>
                <option value="34">API 34 (Android 14)</option>
                <option value="35">API 35 (Android 15)</option>
              </select>
            </div>
            {#if +newProjMinSdk > +newProjTargetSdk || +newProjTargetSdk > +newProjCompileSdk}
              <div class="sdk-warning">需满足 Min ≤ Target ≤ Compile</div>
            {/if}
            <div class="form-item">
              <label>初始模板 (Activity Template)</label>
              <select bind:value={newProjTemplate}>
                <option value="empty_views">Empty Views Activity (含 MainActivity 与布局)</option>
                <option value="trampoline">Trampoline (有桌面入口，无应用内界面)</option>
                <option value="none">No Activity (仅生成基础骨架)</option>
              </select>
            </div>
          </div>
        {/if}
        <div class="modal-buttons">
          <button class="m-btn cancel" on:click={() => (showNewProjModal = false)}>取消</button>
          <button class="m-btn confirm" on:click={executeCreateProject} disabled={isSubmitting || (newProjType === 'android' && (+newProjMinSdk > +newProjTargetSdk || +newProjTargetSdk > +newProjCompileSdk))}>
            {isSubmitting ? '创建中...' : '确认创建'}
          </button>
        </div>
      </div>
    </div>
  {/if}

  <!-- 模态框 2：导入项目向导 (本地目录 / Git Clone) -->
  {#if showImportProjModal}
    <div class="modal-backdrop">
      <div class="modal-box">
        <div class="modal-title">导入已有项目</div>
        <div class="form-item">
          <label>导入方式</label>
          <div class="radio-group">
            <label class="radio-label">
              <input type="radio" bind:group={importMode} value="local" /> 📂 本地已有目录
            </label>
            <label class="radio-label">
              <input type="radio" bind:group={importMode} value="clone" /> 🌐 Git 远程克隆
            </label>
          </div>
        </div>

        {#if importMode === 'local'}
          <div class="form-item">
            <label>本地项目目录路径</label>
            <div class="input-with-btn"><input type="text" bind:value={importLocalPath} placeholder="如：~/my-existing-app" /><button type="button" class="browse-btn" on:click={() => openFolderPicker('importLocalPath', importLocalPath)}>📂 浏览</button></div>
          </div>
        {:else}
          <div class="form-item">
            <label>Git 仓库地址 (HTTPS 或 SSH)</label>
            <input type="text" bind:value={importGitURL} placeholder="如：https://github.com/user/repo.git" />
          </div>
          <div class="form-item">
            <label>存放父目录</label>
            <div class="input-with-btn"><input type="text" bind:value={importGitDir} placeholder="~" /><button type="button" class="browse-btn" on:click={() => openFolderPicker('importGitDir', importGitDir)}>📂 浏览</button></div>
          </div>
          <div class="modal-warning-tip">
            💡 克隆执行期间请保持网络畅通，完成后将自动切入该工作区。
          </div>
        {/if}

        <div class="modal-buttons">
          <button class="m-btn cancel" on:click={() => (showImportProjModal = false)}>取消</button>
          <button class="m-btn confirm" on:click={executeImportProject} disabled={isSubmitting}>
            {isSubmitting ? '导入中...' : '确认导入'}
          </button>
        </div>
      </div>
    </div>
  {/if}

  <!-- 模态框 3：安全删除确认 -->
  {#if showDeleteModal && projectToDelete}
    <div class="modal-backdrop">
      <div class="modal-box">
        <div class="modal-title warning">确认移除项目？</div>
        <p class="modal-desc">
          确定要从 Mobile Studio 列表中移除项目 <strong>{projectToDelete.name}</strong> 吗？
        </p>
        <p class="modal-warning-tip">
          ⚠️ 注意：此操作<strong>仅仅是从 Mobile Studio 的项目列表中移除记录</strong>，磁盘上的实际项目文件不会被删除，需手动删除。
        </p>
        <div class="modal-buttons">
          <button class="m-btn cancel" on:click={() => (showDeleteModal = false)}>取消</button>
          <button class="m-btn danger" on:click={executeDeleteProject}>确认移除</button>
        </div>
      </div>
    </div>
  {/if}

  <!-- 模态框 4：新建文件/文件夹 -->
  {#if showCreateModal && createTargetNode}
    <div class="modal-backdrop">
      <div class="modal-box">
        <div class="modal-title">在 [{createTargetNode.name}] 中新建</div>
        <div class="form-item">
          <label>类型</label>
          <div class="radio-group">
            <label class="radio-label">
              <input type="radio" bind:group={createItemType} value="file" /> 📄 新建文件
            </label>
            <label class="radio-label">
              <input type="radio" bind:group={createItemType} value="dir" /> 📁 新建文件夹
            </label>
          </div>
        </div>
        <div class="form-item">
          <label>名称</label>
          <input type="text" bind:value={createItemName} placeholder="输入名称 (如 layout 或 test.go)" />
        </div>
        <div class="modal-buttons">
          <button class="m-btn cancel" on:click={() => (showCreateModal = false)}>取消</button>
          <button class="m-btn confirm" on:click={executeCreateItem} disabled={isSubmitting}>
            {isSubmitting ? '创建中...' : '确认创建'}
          </button>
        </div>
      </div>
    </div>
  {/if}

  <!-- 模态框 5：长按重命名 -->
  {#if showRenameModal && renameTargetNode}
    <div class="modal-backdrop">
      <div class="modal-box">
        <div class="modal-title">重命名</div>
        <div class="form-item">
          <label>新名称</label>
          <input type="text" bind:value={renameNewName} />
        </div>
        <div class="modal-buttons">
          <button class="m-btn cancel" on:click={() => (showRenameModal = false)}>取消</button>
          <button class="m-btn confirm" on:click={executeRenameItem} disabled={isSubmitting}>
            {isSubmitting ? '保存中...' : '确认重命名'}
          </button>
        </div>
      </div>
    </div>
  {/if}

  <!-- 模态框 6：文件夹选择器 -->
  {#if showFolderPicker}
    <div class="modal-backdrop picker-backdrop">
      <div class="modal-box picker-box">
        <div class="modal-title">选择文件夹</div>
        <div class="picker-current-path" title={pickerCurrent}>{pickerCurrent}</div>
        <div class="picker-list">
          {#if pickerParent && pickerParent !== pickerCurrent}
            <!-- svelte-ignore a11y-click-events-have-key-events -->
            <!-- svelte-ignore a11y-no-static-element-interactions -->
            <div class="picker-row parent-row" on:click={() => browseFolder(pickerParent)}>
              <span>⬆️</span>
              <span class="p-dir-name">.. (返回上一级)</span>
            </div>
          {/if}
          {#if isPickerLoading}
            <div class="empty-hint">正在读取目录...</div>
          {:else if pickerDirs.length === 0}
            <div class="empty-hint">(当前目录下无子文件夹)</div>
          {:else}
            {#each pickerDirs as d}
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div class="picker-row" on:click={() => browseFolder(d.path)}>
                <span>📁</span>
                <span class="p-dir-name">{d.name}</span>
              </div>
            {/each}
          {/if}
        </div>
        <div class="modal-buttons">
          <button class="m-btn cancel" on:click={() => (showFolderPicker = false)}>取消</button>
          <button class="m-btn confirm" on:click={confirmFolderPicker}>✅ 选择当前目录</button>
        </div>
      </div>
    </div>
  {/if}

{:else if node}
  <div class="tree-node">
    {#if node.isDir}
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div
        class="node-row dir {IGNORED_NAMES.has(node.name) ? 'ignored-dir' : ''} {node.isProjectRoot ? 'project-root-row' : ''}"
        on:click={(e) => toggleDir(node, e)}
        on:touchstart={(e) => handleTouchStart(node, e)}
        on:touchmove={handleTouchMove}
        on:touchend={handleTouchEnd}
        on:contextmenu|preventDefault={() => openRenameModal(node)}
      >
        <span class="arrow">{expandedDirs[node.path] ? '▼' : '▶'}</span>
        <span class="folder-icon">{expandedDirs[node.path] ? '📂' : '📁'}</span>
        <span class="name">{node.name}</span>

        {#if IGNORED_NAMES.has(node.name)}
          <span class="ignore-tag">ignore</span>
        {/if}

        {#if loadingDirs[node.path]}
          <span class="layer-loading">...</span>
        {/if}

        <button
          class="add-item-btn"
          on:click|stopPropagation={() => openCreateModal(node)}
          title="在此文件夹内新建"
        >
          +
        </button>
      </div>

      {#if expandedDirs[node.path]}
        <div class="children-block">
          {#if node.children && node.children.length > 0}
            {#each node.children as child}
              <svelte:self
                node={child}
                bind:selectedPath
                bind:expandedDirs
                bind:loadingDirs
                {currentProject}
                {openCreateModal}
                {openRenameModal}
                isRoot={false}
              />
            {/each}
          {:else if !loadingDirs[node.path]}
            <div class="empty-hint">(空文件夹)</div>
          {/if}
        </div>
      {/if}
    {:else}
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div
        class="node-row file {selectedPath === node.path ? 'selected' : ''}"
        on:click={(e) => { e.stopPropagation(); selectedPath = node.path; }}
        on:touchstart={(e) => handleTouchStart(node, e)}
        on:touchmove={handleTouchMove}
        on:touchend={handleTouchEnd}
        on:contextmenu|preventDefault={() => openRenameModal(node)}
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
    width: 280px;
    max-width: 86vw;
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
    padding: 0 10px;
    border-bottom: 1px solid #1e293b;
    background: rgba(15, 23, 42, 0.6);
  }

  .project-item.missing .p-name { color: #64748b; }
  .p-missing { color: #f87171; font-size: 10px; margin-left: 4px; }

  .project-switch-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    font-weight: 700;
    color: #f8fafc;
    cursor: pointer;
    padding: 4px 6px;
    border-radius: 4px;
    max-width: 170px;
    overflow: hidden;
  }

  .project-switch-btn:hover {
    background: #1e293b;
  }

  .proj-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .switch-arrow {
    font-size: 8px;
    color: #94a3b8;
  }

  .header-actions {
    display: flex;
    align-items: center;
    gap: 2px;
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

  .project-drawer {
    background: #0f172a;
    border-bottom: 1px solid #334155;
    padding: 8px 10px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    box-shadow: inset 0 2px 6px rgba(0,0,0,0.4);
  }

  .drawer-section-title {
    font-size: 10px;
    font-weight: 700;
    color: #64748b;
    text-transform: uppercase;
  }

  .project-list {
    max-height: 120px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }

  .project-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 4px 6px;
    border-radius: 4px;
    background: #1e293b;
    cursor: pointer;
    font-size: 11px;
  }

  .project-item.active {
    background: #0284c7;
    color: #ffffff;
  }

  .proj-info {
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .p-name {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .p-path {
    font-size: 9px;
    opacity: 0.7;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .del-proj-btn {
    background: transparent;
    border: none;
    font-size: 12px;
    cursor: pointer;
    padding: 2px 4px;
    opacity: 0.7;
  }

  .del-proj-btn:hover {
    opacity: 1;
  }

  .project-actions {
    display: flex;
    gap: 6px;
    margin-top: 4px;
  }

  .act-btn {
    flex: 1;
    background: #1e293b;
    color: #38bdf8;
    border: 1px solid #334155;
    padding: 4px;
    border-radius: 4px;
    font-size: 10px;
    font-weight: 600;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 3px;
  }

  .act-btn.import {
    color: #34d399;
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
    padding: 0 6px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 12px;
    color: #cbd5e1;
    transition: background 0.1s;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    position: relative;
  }

  .node-row:hover {
    background: #131d33;
  }

  .project-root-row {
    font-weight: 700;
    color: #38bdf8;
    background: rgba(56, 189, 248, 0.08);
    margin-bottom: 2px;
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
    flex: 1;
  }

  .add-item-btn {
    background: transparent;
    border: 1px solid rgba(51, 65, 85, 0.6);
    color: #38bdf8;
    width: 18px;
    height: 18px;
    border-radius: 3px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 12px;
    cursor: pointer;
    opacity: 0.6;
    margin-left: auto;
  }

  .add-item-btn:hover {
    opacity: 1;
    background: #1e293b;
    border-color: #38bdf8;
  }

  .ignore-tag {
    font-size: 8px;
    color: #475569;
    border: 1px solid #334155;
    padding: 0 3px;
    border-radius: 3px;
    margin-right: 4px;
  }

  .layer-loading {
    font-size: 9px;
    color: #38bdf8;
    font-style: italic;
    margin-right: 4px;
  }

  .empty-hint {
    font-size: 10px;
    color: #475569;
    padding: 4px 8px;
    font-style: italic;
  }

  .node-row.file.selected {
    background: #1e293b;
    color: #38bdf8;
    font-weight: 600;
    box-shadow: inset 2px 0 0 #38bdf8;
  }

  .children-block {
    padding-left: 12px;
    border-left: 1px solid rgba(51, 65, 85, 0.4);
    margin-left: 10px;
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

  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.7);
    z-index: 300;
    display: flex;
    align-items: center;
    justify-content: center;
    backdrop-filter: blur(4px);
    padding: 16px;
  }

  .modal-box {
    background: #0f172a;
    border: 1px solid #334155;
    border-radius: 12px;
    width: 320px;
    max-width: 90vw;
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    box-shadow: 0 8px 32px rgba(0,0,0,0.8);
  }

  .modal-title {
    font-size: 14px;
    font-weight: 700;
    color: #f8fafc;
  }

  .modal-title.warning {
    color: #f87171;
  }

  .modal-desc {
    font-size: 12px;
    color: #cbd5e1;
    margin: 0;
  }

  .modal-warning-tip {
    font-size: 11px;
    color: #fbbf24;
    background: rgba(251, 191, 36, 0.1);
    border-left: 3px solid #fbbf24;
    padding: 6px 8px;
    margin: 0;
    border-radius: 2px;
    line-height: 1.4;
  }

  .form-item {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .form-item label {
    font-size: 11px;
    font-weight: 600;
    color: #94a3b8;
  }

  .form-item input[type="text"] {
    background: #1e293b;
    border: 1px solid #334155;
    color: #f8fafc;
    padding: 6px 8px;
    border-radius: 6px;
    font-size: 12px;
    outline: none;
  }

  .form-item input[type="text"]:focus {
    border-color: #38bdf8;
  }

  .radio-group {
    display: flex;
    gap: 14px;
    margin-top: 2px;
  }

  .radio-label {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    color: #e2e8f0;
    cursor: pointer;
  }

  .modal-buttons {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 6px;
  }

  .m-btn {
    padding: 6px 12px;
    border-radius: 6px;
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
    border: none;
  }

  .m-btn.cancel {
    background: #1e293b;
    color: #94a3b8;
  }

  .m-btn.confirm {
    background: #0284c7;
    color: #ffffff;
  }

  .m-btn.danger {
    background: #dc2626;
    color: #ffffff;
  }

  .input-with-btn {
    display: flex;
    gap: 6px;
    align-items: center;
  }

  .input-with-btn input[type="text"] {
    flex: 1;
    min-width: 0;
  }

  .browse-btn {
    background: #1e293b;
    border: 1px solid #38bdf8;
    color: #38bdf8;
    padding: 6px 10px;
    border-radius: 6px;
    font-size: 11px;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
  }

  .browse-btn:active {
    background: #0284c7;
    color: #ffffff;
  }

  .picker-backdrop {
    z-index: 350;
  }

  .picker-box {
    width: 340px;
    max-height: 80vh;
  }

  .picker-current-path {
    font-size: 11px;
    color: #38bdf8;
    background: #090d16;
    border: 1px solid #1e293b;
    padding: 6px 8px;
    border-radius: 6px;
    word-break: break-all;
    font-family: monospace;
  }

  .picker-list {
    max-height: 240px;
    overflow-y: auto;
    background: #090d16;
    border: 1px solid #1e293b;
    border-radius: 6px;
    padding: 4px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .picker-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 7px 8px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 12px;
    color: #e2e8f0;
  }

  .picker-row:hover, .picker-row:active {
    background: #1e293b;
    color: #38bdf8;
  }

  .picker-row.parent-row {
    color: #94a3b8;
    border-bottom: 1px solid #1e293b;
    font-weight: 600;
  }

  .p-dir-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
  }

  .android-config-panel {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 10px;
    background: rgba(15, 23, 42, 0.75);
    border: 1px solid #1e293b;
    border-left: 3px solid #38bdf8;
    border-radius: 6px;
  }

  .sdk-warning {
    font-size: 11px;
    color: #f87171;
  }

  .sdk-row {
    display: flex;
    gap: 8px;
  }

  .form-item.half {
    flex: 1;
    min-width: 0;
  }

  .form-item select {
    background: #1e293b;
    border: 1px solid #334155;
    color: #f8fafc;
    padding: 6px 8px;
    border-radius: 6px;
    font-size: 11px;
    outline: none;
  }

  .form-item select:focus {
    border-color: #38bdf8;
  }
</style>
