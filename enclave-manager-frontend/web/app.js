const STORAGE_TASKS_KEY = "enclave-manager-frontend.tasks";
const STORAGE_SELECTED_KEY = "enclave-manager-frontend.selected";
const MAX_RECENT_TASKS = 8;
const POLL_INTERVAL_MS = 4000;
const TERMINAL_STATES = new Set(["DONE", "FAILED", "MISSING"]);

const STATUS_META = {
  UNKNOWN: {
    label: "等待同步",
    tone: "muted",
    description: "任务已经加入看板，但还没有从 Manager 拉取到有效状态。",
  },
  MISSING: {
    label: "任务不存在",
    tone: "muted",
    description: "Manager 没有找到这个 task_id，可能已经过期或服务已重启。",
  },
  CODE_UPLOADED: {
    label: "代码已上传",
    tone: "info",
    description: "tar.gz 已成功上传并创建 task_id，可以继续启动 Enclave。",
  },
  STARTING_ENCLAVE: {
    label: "正在启动 Enclave",
    tone: "warning",
    description: "Manager 正在初始化 Occlum 环境并拉起 Enclave 进程。",
  },
  ENCLAVE_RUNNING: {
    label: "Enclave 运行中",
    tone: "success",
    description: "Enclave 已启动完成，RA-TLS 端口已分配，可以等待 Data Connector 接入。",
  },
  DATA_RECEIVED: {
    label: "已收到数据",
    tone: "warning",
    description: "Data Connector 已通过 RA-TLS 把数据送入 Enclave，任务正在处理中。",
  },
  DONE: {
    label: "任务完成",
    tone: "success",
    description: "Enclave 已完成数据处理，结果已经返回给 Data Connector。",
  },
  FAILED: {
    label: "任务失败",
    tone: "danger",
    description: "启动或运行过程中发生错误，请查看错误详情。",
  },
};

const TIMELINE_STEPS = [
  {
    key: "CODE_UPLOADED",
    title: "接收代码包",
    note: "已生成 task_id，并将上传内容暂存到 Manager。",
  },
  {
    key: "STARTING_ENCLAVE",
    title: "初始化 Enclave",
    note: "Occlum 正在准备镜像、配置和分配 RA-TLS 端口。",
  },
  {
    key: "ENCLAVE_RUNNING",
    title: "进入可连接状态",
    note: "Enclave 已启动，Data Connector 可以开始连接。",
  },
  {
    key: "DATA_RECEIVED",
    title: "接收安全数据",
    note: "Data Connector 已通过 RA-TLS 将数据送入 Enclave，任务正在处理。",
  },
  {
    key: "DONE",
    title: "完成处理",
    note: "处理结果已经回传，任务流进入完成状态。",
  },
];

const state = {
  config: {
    managerBaseUrl: "http://127.0.0.1:8081",
    dataConnectorBaseUrl: "http://127.0.0.1:8082",
    proxyBasePath: "/api",
    dataProxyBasePath: "/connector",
    frontendPort: window.location.port || "5174",
  },
  taskOrder: [],
  tasks: new Map(),
  selectedTaskId: "",
  uploadBusy: false,
  activeUploadIntent: "",
  pollTimer: null,
};

const elements = {};
const timeFormatter = new Intl.DateTimeFormat("zh-CN", {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
});

document.addEventListener("DOMContentLoaded", () => {
  cacheDom();
  bindEvents();
  init().catch((error) => {
    showToast(`页面初始化失败：${error.message}`, "error");
  });
});

async function init() {
  await loadConfig();
  hydrateState();
  render();

  if (state.taskOrder.length > 0) {
    await refreshTasks(state.taskOrder, { silent: true });
  }

  render();
  startPolling();
}

function cacheDom() {
  const q = (selector) => document.querySelector(selector);

  elements.connectionSummary = q("#connectionSummary");
  elements.uploadForm = q("#uploadForm");
  elements.archiveInput = q("#archiveInput");
  elements.uploadAndStartButton = q("#uploadAndStartButton");
  elements.uploadOnlyButton = q("#uploadOnlyButton");
  elements.trackForm = q("#trackForm");
  elements.trackTaskId = q("#trackTaskId");
  elements.selectedTaskPanel = q("#selectedTaskPanel");
  elements.recentTasksList = q("#recentTasksList");
  elements.nextStepsPanel = q("#nextStepsPanel");
  elements.toastRegion = q("#toastRegion");
  elements.dataForm = q("#dataForm");
  elements.targetUrlInput = q("#targetUrlInput");
  elements.csvInput = q("#csvInput");
  elements.dataSubmitButton = q("#dataSubmitButton");
  elements.dataResult = q("#dataResult");
}

function bindEvents() {
  if (elements.uploadForm) {
    elements.uploadForm.addEventListener("submit", handleUploadSubmit);
  }
  if (elements.trackForm) {
    elements.trackForm.addEventListener("submit", handleTrackSubmit);
  }
  if (elements.dataForm) {
    elements.dataForm.addEventListener("submit", handleDataSubmit);
  }

  document.body.addEventListener("click", async (event) => {
    const clickTarget = event.target instanceof Element ? event.target : event.target?.parentElement;
    const actionButton = clickTarget?.closest("[data-action]");
    if (!actionButton) {
      return;
    }

    const { action, taskId, copyText } = actionButton.dataset;

    if (action === "select-task" && taskId) {
      setSelectedTask(taskId);
      return;
    }

    if (action === "refresh-task" && taskId) {
      try {
        await refreshTask(taskId);
      } catch (error) {
        showToast(`刷新任务失败：${error.message}`, "error");
      }
      return;
    }

    if (action === "remove-task" && taskId) {
      removeTask(taskId);
      render();
      return;
    }

    if (action === "start-task" && taskId) {
      await startTask(taskId);
      return;
    }

    if (action === "copy-text" && typeof copyText === "string") {
      await copyToClipboard(decodeURIComponent(copyText));
    }
  });
}

async function loadConfig() {
  try {
    const config = await request("/app-config");
    state.config = {
      managerBaseUrl: config.managerBaseUrl || state.config.managerBaseUrl,
      dataConnectorBaseUrl:
        config.dataConnectorBaseUrl ||
        deriveDataConnectorBaseURL(config.managerBaseUrl || state.config.managerBaseUrl),
      proxyBasePath: config.proxyBasePath || "/api",
      dataProxyBasePath: config.dataProxyBasePath || "/connector",
      frontendPort: config.frontendPort || state.config.frontendPort,
    };
  } catch (error) {
    showToast(`读取前端配置失败，已回退到默认地址：${error.message}`, "error");
  }
}

function hydrateState() {
  state.taskOrder = readJSON(STORAGE_TASKS_KEY, []).filter(Boolean).slice(0, MAX_RECENT_TASKS);
  state.selectedTaskId = window.localStorage.getItem(STORAGE_SELECTED_KEY) || state.taskOrder[0] || "";

  state.taskOrder.forEach((taskId) => {
    if (!state.tasks.has(taskId)) {
      state.tasks.set(taskId, createTaskShell(taskId));
    }
  });
}

function createTaskShell(taskId) {
  return {
    task_id: taskId,
    status: "UNKNOWN",
    port: null,
    error: "",
    lastNonFailedStatus: "",
    lastSyncedAt: null,
    uiPending: false,
    events: [],
  };
}

async function handleUploadSubmit(event) {
  event.preventDefault();

  const file = elements.archiveInput?.files?.[0];
  if (!file) {
    showToast("请先选择一个 tar.gz 文件。", "error");
    return;
  }

  const intent = event.submitter?.value || "upload_and_start";
  state.uploadBusy = true;
  state.activeUploadIntent = intent;
  renderUploadButtons();

  try {
    const uploadResult = await request("/api/upload-code", {
      method: "POST",
      headers: {
        "Content-Type": "application/octet-stream",
      },
      body: file,
    });

    const taskId = uploadResult.task_id;
    rememberTask(taskId);
    applyTaskPatch(
      taskId,
      {
        status: "CODE_UPLOADED",
        error: "",
        port: null,
        lastSyncedAt: Date.now(),
      },
      "代码包上传成功",
    );
    setSelectedTask(taskId);
    elements.archiveInput.value = "";

    showToast(`任务 ${shortTaskId(taskId)} 已创建。`);

    if (intent === "upload_and_start") {
      await startTask(taskId);
    } else {
      await refreshTask(taskId, { silent: true }).catch(() => undefined);
      render();
    }
  } catch (error) {
    showToast(`上传失败：${error.message}`, "error");
  } finally {
    state.uploadBusy = false;
    state.activeUploadIntent = "";
    renderUploadButtons();
  }
}

async function handleTrackSubmit(event) {
  event.preventDefault();

  const taskId = elements.trackTaskId.value.trim();
  if (!taskId) {
    showToast("请输入要跟踪的 task_id。", "error");
    return;
  }

  rememberTask(taskId);
  setSelectedTask(taskId);
  elements.trackTaskId.value = "";
  render();

  try {
    await refreshTask(taskId, { silent: true });
    showToast(`任务 ${shortTaskId(taskId)} 已加入看板。`);
  } catch (error) {
    showToast(`加入看板完成，但首次同步失败：${error.message}`, "error");
  }
}

async function handleDataSubmit(event) {
  event.preventDefault();

  const file = elements.csvInput?.files?.[0];
  const taskId = elements.targetUrlInput?.value.trim();

  if (!file || !taskId) {
    showToast("请同时提供 Task ID 和 CSV 文件。", "error");
    return;
  }

  const connectorUrl = getDataConnectorForwardUrl();
  elements.dataSubmitButton.disabled = true;
  elements.dataSubmitButton.textContent = "正在发送数据...";
  elements.dataResult.innerHTML = `<p class="info-text">正在通过 ${escapeHTML(connectorUrl)} 发送任务 [${escapeHTML(taskId)}] 和 CSV 数据...</p>`;

  const formData = new FormData();
  formData.append("task_id", taskId);
  formData.append("file", file);

  try {
    const rawResponse = await fetch(connectorUrl, {
      method: "POST",
      body: formData,
    });

    const textResult = await rawResponse.text();
    if (!rawResponse.ok) {
      throw new Error(`代理错误 (${rawResponse.status})：${textResult}`);
    }

    elements.dataResult.innerHTML = `<h3>Enclave 返回结果</h3><pre>${escapeHTML(textResult)}</pre>`;
    showToast("数据处理成功。");
  } catch (error) {
    elements.dataResult.innerHTML = `<p class="task-error">请求失败：${escapeHTML(error.message)}</p>`;
    showToast(`数据提交失败：${error.message}`, "error");
  } finally {
    elements.dataSubmitButton.disabled = false;
    elements.dataSubmitButton.textContent = "安全提交";
  }
}

async function startTask(taskId) {
  const currentTask = ensureTask(taskId);
  if (currentTask.uiPending) {
    return;
  }

  applyTaskPatch(taskId, { uiPending: true }, "已发送启动请求");
  render();

  try {
    const startResult = await request("/api/start-enclave", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ task_id: taskId }),
    });

    applyTaskPatch(
      taskId,
      {
        status: "STARTING_ENCLAVE",
        port: startResult.port || null,
        error: "",
        uiPending: false,
        lastSyncedAt: Date.now(),
        lastNonFailedStatus: "STARTING_ENCLAVE",
      },
      `Enclave 已启动，正在等待 RA-TLS 就绪，端口 ${startResult.port}`,
    );

    showToast(`Enclave 启动请求已提交，正在等待 RA-TLS 就绪，端口 ${startResult.port}。`);
    await refreshTask(taskId, { silent: true }).catch(() => undefined);
  } catch (error) {
    applyTaskPatch(
      taskId,
      {
        status: "FAILED",
        error: error.message,
        uiPending: false,
        lastSyncedAt: Date.now(),
        lastNonFailedStatus: currentTask.lastNonFailedStatus || "CODE_UPLOADED",
      },
      `启动失败：${error.message}`,
    );

    showToast(`启动失败：${error.message}`, "error");
  }
}

async function refreshTasks(taskIds, options = {}) {
  const uniqueTaskIds = [...new Set(taskIds)].filter(Boolean);
  await Promise.all(uniqueTaskIds.map((taskId) => refreshTask(taskId, options).catch(() => undefined)));
}

async function refreshTask(taskId, { silent = false } = {}) {
  try {
    const task = await request(`/api/task-status?task_id=${encodeURIComponent(taskId)}`);

    const existingTask = ensureTask(taskId);
    const nextStatus = task.status || existingTask.status || "UNKNOWN";
    const normalizedError =
      typeof task.error === "string" ? task.error : nextStatus === "FAILED" ? existingTask.error : "";

    const patch = {
      ...task,
      task_id: taskId,
      status: nextStatus,
      error: normalizedError,
      lastSyncedAt: Date.now(),
      lastNonFailedStatus: nextStatus !== "FAILED" ? nextStatus : existingTask.lastNonFailedStatus,
      uiPending: false,
    };

    applyTaskPatch(taskId, patch);

    if (!silent) {
      showToast(`任务 ${shortTaskId(taskId)} 已刷新。`);
    }
  } catch (error) {
    if (error.message === "Task not found") {
      applyTaskPatch(
        taskId,
        {
          status: "MISSING",
          error: "Manager 中未找到该任务，可能已经过期或服务已重启。",
          lastSyncedAt: Date.now(),
          uiPending: false,
        },
        "Manager 中未找到该任务",
      );

      if (!silent) {
        showToast(`任务 ${shortTaskId(taskId)} 已不存在。`, "error");
      }
      return;
    }

    throw error;
  }
}

function ensureTask(taskId) {
  if (!state.tasks.has(taskId)) {
    state.tasks.set(taskId, createTaskShell(taskId));
  }
  return state.tasks.get(taskId);
}

function applyTaskPatch(taskId, patch, eventMessage = "") {
  const currentTask = ensureTask(taskId);
  const nextStatus = patch.status || currentTask.status || "UNKNOWN";
  const statusChanged = currentTask.status && currentTask.status !== nextStatus;

  const nextTask = {
    ...currentTask,
    ...patch,
    task_id: taskId,
    status: nextStatus,
    error: patch.error !== undefined ? patch.error : currentTask.error,
    port: patch.port !== undefined ? patch.port : currentTask.port,
    lastSyncedAt: patch.lastSyncedAt || currentTask.lastSyncedAt,
    lastNonFailedStatus:
      patch.lastNonFailedStatus !== undefined
        ? patch.lastNonFailedStatus
        : nextStatus !== "FAILED" && nextStatus !== "MISSING" && nextStatus !== "UNKNOWN"
          ? nextStatus
          : currentTask.lastNonFailedStatus,
    events: [...currentTask.events],
  };

  if (statusChanged) {
    prependEvent(nextTask, `状态更新为 ${getStatusMeta(nextStatus).label}`);
  }

  if (eventMessage) {
    prependEvent(nextTask, eventMessage);
  }

  state.tasks.set(taskId, nextTask);
  persistState();
  render();
}

function prependEvent(task, message) {
  task.events.unshift({
    message,
    timestamp: Date.now(),
  });
  task.events = task.events.slice(0, 6);
}

function rememberTask(taskId) {
  ensureTask(taskId);
  state.taskOrder = [taskId, ...state.taskOrder.filter((item) => item !== taskId)].slice(0, MAX_RECENT_TASKS);
  persistState();
}

function removeTask(taskId) {
  state.tasks.delete(taskId);
  state.taskOrder = state.taskOrder.filter((item) => item !== taskId);

  if (state.selectedTaskId === taskId) {
    state.selectedTaskId = state.taskOrder[0] || "";
  }

  persistState();
}

function setSelectedTask(taskId) {
  state.selectedTaskId = taskId;
  persistState();
  render();
}

function persistState() {
  window.localStorage.setItem(STORAGE_TASKS_KEY, JSON.stringify(state.taskOrder));
  if (state.selectedTaskId) {
    window.localStorage.setItem(STORAGE_SELECTED_KEY, state.selectedTaskId);
  } else {
    window.localStorage.removeItem(STORAGE_SELECTED_KEY);
  }
}

function render() {
  if (elements.connectionSummary) {
    renderConnectionSummary();
  }
  if (elements.uploadForm) {
    renderUploadButtons();
  }
  if (elements.selectedTaskPanel) {
    renderSelectedTask();
  }
  if (elements.recentTasksList) {
    renderRecentTasks();
  }
  if (elements.nextStepsPanel) {
    renderNextSteps();
  }
}

function renderUploadButtons() {
  if (!elements.uploadAndStartButton || !elements.uploadOnlyButton) {
    return;
  }

  const busyIntent = state.activeUploadIntent;

  elements.uploadAndStartButton.disabled = state.uploadBusy;
  elements.uploadOnlyButton.disabled = state.uploadBusy;

  elements.uploadAndStartButton.textContent =
    state.uploadBusy && busyIntent === "upload_and_start" ? "正在上传并启动..." : "上传并启动";
  elements.uploadOnlyButton.textContent =
    state.uploadBusy && busyIntent === "upload_only" ? "正在上传..." : "仅上传代码";
}

function renderConnectionSummary() {
  const selectedTask = getSelectedTask();
  const selectedMeta = selectedTask ? getStatusMeta(selectedTask.status) : getStatusMeta("UNKNOWN");

  const managerUrl = parseManagerUrl(state.config.managerBaseUrl);
  const managerHost = managerUrl ? managerUrl.hostname : "127.0.0.1";
  const raTlsEndpoint = selectedTask && selectedTask.port ? `${managerHost}:${selectedTask.port}` : "等待分配";
  const dataConnectorUrl =
    state.config.dataConnectorBaseUrl || deriveDataConnectorBaseURL(state.config.managerBaseUrl);

  elements.connectionSummary.innerHTML = `
    <div class="connection-card">
      <div class="connection-row">
        <div>
          <p class="panel-kicker">代理路由</p>
          <h3>当前路由</h3>
        </div>
        <span class="status-badge tone-${selectedMeta.tone}">${escapeHTML(selectedTask ? selectedMeta.label : "未选择任务")}</span>
      </div>
      <div class="connection-route">${escapeHTML(state.config.managerBaseUrl)}</div>
      <div class="connection-grid">
        <div>
          <span>RA-TLS 端点</span>
          <strong>${escapeHTML(raTlsEndpoint)}</strong>
        </div>
        <div>
          <span>数据代理</span>
          <strong>${escapeHTML(state.config.dataProxyBasePath || "/connector")}</strong>
        </div>
        <div>
          <span>当前任务</span>
          ${
            selectedTask
              ? `<button class="button-subtle" data-action="copy-text" data-copy-text="${encodeCopy(selectedTask.task_id)}" title="点击复制 Task ID">${escapeHTML(selectedTask.task_id)}</button>`
              : `<strong>无</strong>`
          }
        </div>
        <div>
          <span>已跟踪任务</span>
          <strong>${state.taskOrder.length}</strong>
        </div>
      </div>
      <p>
        浏览器会先把 CSV 数据提交到当前前端服务的同源代理，再由服务端转发到远端
        <code>data-connector</code>，完成后续的 RA-TLS 投递。
      </p>
    </div>
  `;
}

function renderSelectedTask() {
  const task = getSelectedTask();
  if (!task) {
    elements.selectedTaskPanel.innerHTML = `
      <div class="empty-state">
        <h3>还没有选中任务</h3>
        <p>上传一个新的 Enclave 包，或者把已有 task_id 加入看板后即可查看详情。</p>
      </div>
    `;
    return;
  }

  const meta = getStatusMeta(task.status);
  const canStart = ["CODE_UPLOADED", "FAILED", "DONE", "ENCLAVE_RUNNING"].includes(task.status);
  const startButtonText = task.status === "CODE_UPLOADED" ? "启动 Enclave" : "重新启动";
  const events =
    task.events.length > 0
      ? task.events
          .map(
            (item) => `
        <div class="event-row">
          <span>${escapeHTML(item.message)}</span>
          <time>${escapeHTML(formatTime(item.timestamp))}</time>
        </div>
      `,
          )
          .join("")
      : `
      <div class="event-row">
        <span>暂无本地事件记录，等待下一次操作或同步。</span>
        <time>--:--:--</time>
      </div>
    `;

  elements.selectedTaskPanel.innerHTML = `
    <div class="task-main-head">
      <div class="task-title">
        <p class="panel-kicker">当前聚焦任务</p>
        <h3>${escapeHTML(shortTaskId(task.task_id))}</h3>
        <div class="task-id">${escapeHTML(task.task_id)}</div>
        <span class="status-badge tone-${meta.tone}">${escapeHTML(meta.label)}</span>
      </div>
      <div class="task-actions">
        <button class="button-subtle" data-action="refresh-task" data-task-id="${escapeHTML(task.task_id)}">刷新</button>
        <button class="button-subtle" data-action="copy-text" data-copy-text="${encodeCopy(task.task_id)}">复制 ID</button>
        ${canStart ? `
          <button class="button-subtle" data-action="start-task" data-task-id="${escapeHTML(task.task_id)}" ${task.uiPending ? "disabled" : ""}>
            ${task.uiPending ? "操作中..." : startButtonText}
          </button>
        ` : ""}
      </div>
    </div>

    <div class="detail-grid">
      <div class="detail-card">
        <span>任务状态</span>
        <strong>${escapeHTML(meta.label)}</strong>
        <p>${escapeHTML(meta.description)}</p>
      </div>
      <div class="detail-card">
        <span>RA-TLS 端口</span>
        <strong>${escapeHTML(task.port ? String(task.port) : "未分配")}</strong>
        <p>只有启动成功后才会获得动态端口。</p>
      </div>
      <div class="detail-card">
        <span>最近同步</span>
        <strong>${escapeHTML(task.lastSyncedAt ? formatTime(task.lastSyncedAt) : "尚未同步")}</strong>
        <p>自动轮询会持续刷新非终态任务。</p>
      </div>
    </div>

    ${task.error ? `<div class="error-banner">${escapeHTML(task.error)}</div>` : ""}

    <div class="timeline">
      ${renderTimeline(task)}
    </div>

    <div class="event-card">
      <h3>本地操作记录</h3>
      <div class="event-list">${events}</div>
    </div>
  `;
}

function renderTimeline(task) {
  const activeIndex = getTimelineIndex(task.status === "FAILED" ? task.lastNonFailedStatus : task.status);
  const failedIndex = task.status === "FAILED" ? Math.min(activeIndex + 1, TIMELINE_STEPS.length - 1) : -1;

  return TIMELINE_STEPS.map((step, index) => {
    let stepClass = "upcoming";

    if (task.status === "DONE") {
      stepClass = "complete";
    } else if (task.status === "FAILED") {
      if (index <= activeIndex) {
        stepClass = "complete";
      } else if (index === failedIndex) {
        stepClass = "fail";
      }
    } else if (activeIndex >= 0) {
      if (index < activeIndex) {
        stepClass = "complete";
      } else if (index === activeIndex) {
        stepClass = "current";
      }
    }

    return `
      <article class="timeline-step ${stepClass}">
        <div class="step-index">${String(index + 1).padStart(2, "0")}</div>
        <div>
          <h4>${escapeHTML(step.title)}</h4>
          <p>${escapeHTML(step.note)}</p>
        </div>
      </article>
    `;
  }).join("");
}

function renderRecentTasks() {
  if (state.taskOrder.length === 0) {
    elements.recentTasksList.innerHTML = `
      <div class="empty-state compact">
        <p>暂无最近任务，先上传一个 Enclave 包吧。</p>
      </div>
    `;
    return;
  }

  elements.recentTasksList.innerHTML = state.taskOrder
    .map((taskId) => {
      const task = ensureTask(taskId);
      const meta = getStatusMeta(task.status);
      const isSelected = taskId === state.selectedTaskId;

      return `
        <article class="task-card ${isSelected ? "selected" : ""}" data-action="select-task" data-task-id="${escapeHTML(task.task_id)}" style="cursor: pointer;">
          <div class="task-card-head">
            <div>
              <h3>${escapeHTML(shortTaskId(task.task_id))}</h3>
              <p>${escapeHTML(task.task_id)}</p>
            </div>
            <span class="status-badge tone-${meta.tone}">${escapeHTML(meta.label)}</span>
          </div>

          <div class="task-meta">
            <span>端口：${escapeHTML(task.port ? String(task.port) : "未分配")}</span>
            <span>同步：${escapeHTML(task.lastSyncedAt ? formatTime(task.lastSyncedAt) : "未同步")}</span>
          </div>

          ${task.error ? `<p class="task-error">${escapeHTML(task.error)}</p>` : ""}

          <div class="task-card-actions">
            <button class="button-subtle" data-action="select-task" data-task-id="${escapeHTML(task.task_id)}">
              ${isSelected ? "当前查看中" : "查看详情"}
            </button>
            <button class="button-subtle" data-action="refresh-task" data-task-id="${escapeHTML(task.task_id)}">刷新</button>
            <button class="button-subtle" data-action="remove-task" data-task-id="${escapeHTML(task.task_id)}">移除</button>
          </div>
        </article>
      `;
    })
    .join("");
}

function renderNextSteps() {
  const task = getSelectedTask();
  const managerUrl = parseManagerUrl(state.config.managerBaseUrl);
  const managerHost = managerUrl ? managerUrl.hostname : "127.0.0.1";
  const connectorAddress = task && task.port ? `${managerHost}:${task.port}` : `${managerHost}:<动态端口>`;
  const statusCommand = task
    ? `curl "${state.config.managerBaseUrl}/task-status?task_id=${task.task_id}"`
    : `curl "${state.config.managerBaseUrl}/task-status?task_id=<task_id>"`;
  const dataCommand = `go run ./data-connector ./path/to/data.csv ${connectorAddress}`;
  const buildCommand = [
    "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildmode=pie -o enclave-app main.go",
    "tar -czvf enclave.tar.gz enclave-app",
  ].join("\n");

  elements.nextStepsPanel.innerHTML = `
    <div class="snippet-grid">
      <article class="snippet-card">
        <div class="snippet-head">
          <div>
            <h3>打包待上传的 Enclave</h3>
            <p>先在 <code>enclave-app</code> 目录产出 Linux PIE 二进制，再打成 tar.gz。</p>
          </div>
          <div class="snippet-actions">
            <button class="button-subtle" data-action="copy-text" data-copy-text="${encodeCopy(buildCommand)}">复制命令</button>
          </div>
        </div>
        <pre>${escapeHTML(buildCommand)}</pre>
      </article>

      <article class="snippet-card">
        <div class="snippet-head">
          <div>
            <h3>查询任务状态</h3>
            <p>如果你在命令行或脚本侧也需要跟踪任务，可以直接访问 Manager 的状态接口。</p>
          </div>
          <div class="snippet-actions">
            <button class="button-subtle" data-action="copy-text" data-copy-text="${encodeCopy(statusCommand)}">复制命令</button>
          </div>
        </div>
        <pre>${escapeHTML(statusCommand)}</pre>
      </article>

      <article class="snippet-card">
        <div class="snippet-head">
          <div>
            <h3>通过 Data Connector 发送数据</h3>
            <p>浏览器页面不直接接管 RA-TLS，真正的数据注入仍建议使用仓库里的客户端。</p>
          </div>
          <div class="snippet-actions">
            <button class="button-subtle" data-action="copy-text" data-copy-text="${encodeCopy(dataCommand)}">复制命令</button>
          </div>
        </div>
        <pre>${escapeHTML(dataCommand)}</pre>
      </article>
    </div>

    <article class="status-explainer">
      <h3>使用提醒</h3>
      <p>
        当前 <code>enclave-manager</code> 的 <code>/process-data</code> 已废弃，数据处理改为由
        <code>data-connector</code> 直接通过 RA-TLS 连接到 Enclave 的动态端口。因此这个前端页面主要用于任务发布、
        状态观察和操作引导，而不是替代安全数据通道。
      </p>
    </article>
  `;
}

function getSelectedTask() {
  return state.selectedTaskId ? ensureTask(state.selectedTaskId) : null;
}

function getStatusMeta(status) {
  return STATUS_META[status] || STATUS_META.UNKNOWN;
}

function getTimelineIndex(status) {
  return TIMELINE_STEPS.findIndex((step) => step.key === status);
}

function parseManagerUrl(rawUrl) {
  try {
    return new URL(rawUrl);
  } catch (_error) {
    return null;
  }
}

function deriveDataConnectorBaseURL(managerBaseUrl) {
  const managerUrl = parseManagerUrl(managerBaseUrl);
  if (!managerUrl || !managerUrl.hostname) {
    return "http://127.0.0.1:8082";
  }

  const scheme = managerUrl.protocol || "http:";
  return `${scheme}//${managerUrl.hostname}:8082`;
}

function getDataConnectorForwardUrl() {
  const proxyBasePath = state.config.dataProxyBasePath || "/connector";
  return `${String(proxyBasePath).replace(/\/+$/, "")}/forward`;
}

function shortTaskId(taskId) {
  if (!taskId || taskId.length <= 12) {
    return taskId || "";
  }
  return `${taskId.slice(0, 6)}...${taskId.slice(-4)}`;
}

function formatTime(timestamp) {
  return timeFormatter.format(new Date(timestamp));
}

function startPolling() {
  stopPolling();
  state.pollTimer = window.setInterval(() => {
    const taskIds = state.taskOrder.filter((taskId) => {
      const task = ensureTask(taskId);
      return task && !TERMINAL_STATES.has(task.status) && !task.uiPending;
    });

    if (taskIds.length > 0) {
      refreshTasks(taskIds, { silent: true }).catch(() => undefined);
    }
  }, POLL_INTERVAL_MS);
}

function stopPolling() {
  if (state.pollTimer) {
    window.clearInterval(state.pollTimer);
    state.pollTimer = null;
  }
}

async function request(url, options = {}) {
  const response = await fetch(url, options);
  const rawText = await response.text();
  let parsedBody = rawText;

  if (rawText) {
    try {
      parsedBody = JSON.parse(rawText);
    } catch (_error) {
      parsedBody = rawText;
    }
  }

  if (!response.ok) {
    const message =
      typeof parsedBody === "string"
        ? parsedBody
        : parsedBody?.error || parsedBody?.message || `请求失败 (${response.status})`;

    if (response.status === 404 && /task not found/i.test(String(message))) {
      throw new Error("Task not found");
    }

    throw new Error(message);
  }

  return parsedBody;
}

function readJSON(storageKey, fallback) {
  try {
    const rawValue = window.localStorage.getItem(storageKey);
    return rawValue ? JSON.parse(rawValue) : fallback;
  } catch (_error) {
    return fallback;
  }
}

function encodeCopy(text) {
  return encodeURIComponent(text);
}

async function copyToClipboard(text) {
  try {
    await navigator.clipboard.writeText(text);
    showToast("已复制到剪贴板。");
  } catch (_error) {
    showToast("复制失败，请手动复制。", "error");
  }
}

function showToast(message, tone = "info") {
  if (!elements.toastRegion) {
    return;
  }

  const toast = document.createElement("div");
  toast.className = `toast ${tone === "error" ? "error" : ""}`.trim();
  toast.textContent = message;
  elements.toastRegion.appendChild(toast);

  window.setTimeout(() => {
    toast.remove();
  }, 2600);
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}
