import {
  ListPorts,
  Flash,
  ChooseFile,
  MonitorPort,
  StopMonitor,
  GetAppVersion,
  CheckForUpdate,
  InstallUpdate,
} from "../wailsjs/go/main/App.js";
import { EventsOn } from "../wailsjs/runtime/runtime.js";
import { formatLogCounter, formatTime, language, t, translateDocument } from "./i18n.js";

const portSelect = document.getElementById("portSelect");
const baudSelect = document.getElementById("baudSelect");
const btnRefresh = document.getElementById("btnRefresh");
const btnChoose = document.getElementById("btnChoose");
const btnFlash = document.getElementById("btnFlash");
const btnMonitor = document.getElementById("btnMonitor");
const btnStopMonitor = document.getElementById("btnStopMonitor");
const btnClearLog = document.getElementById("btnClearLog");
const btnAutoScroll = document.getElementById("btnAutoScroll");
const filePath = document.getElementById("filePath");
const logArea = document.getElementById("log");
const progressContainer = document.getElementById("progressContainer");
const progressBar = document.getElementById("progressBar");
const progressText = document.getElementById("progressText");
const systemStatus = document.getElementById("systemStatus");
const logCounter = document.getElementById("logCounter");
const btnUpdate = document.getElementById("btnUpdate");
const appVersionTag = document.getElementById("appVersion");

let isMonitoring = false;
let logUpdateTimeout = null; // Batches log rendering updates.
let autoScrollEnabled = true; // Auto-scroll is enabled by default.
let logLines = []; // In-memory terminal line buffer.
let logCharacterCount = 0;
let lastProgressLog = null;
let availableUpdate = null;
let isFlashing = false;
const LOG_RENDER_INTERVAL_MS = 50;
const MAX_LOG_LINES = 500;
const MAX_LOG_CHARACTERS = 160_000;
const MAX_LOG_LINE_CHARACTERS = 2_048;

// Append a timestamped message to the log.
function log(msg) {
  const timestamp = formatTime(new Date());
  addLogLine(`[${timestamp}] ${msg}`);
}

// Append one line to the terminal efficiently.
function addLogLine(line) {
  appendLogLines([line]);
}

function appendLogLines(lines) {
  for (const value of lines) {
    let line = String(value).trim();
    if (!line) {
      continue;
    }
    if (line.length > MAX_LOG_LINE_CHARACTERS) {
      line = `${line.slice(0, MAX_LOG_LINE_CHARACTERS)} … [${t("log.truncated")}]`;
    }
    logLines.push(line);
    logCharacterCount += line.length;
  }

  while (logLines.length > MAX_LOG_LINES || logCharacterCount > MAX_LOG_CHARACTERS) {
    const removed = logLines.shift();
    if (removed === undefined) {
      break;
    }
    logCharacterCount -= removed.length;
  }

  scheduleLogRender();
}

function scheduleLogRender() {
  if (logUpdateTimeout !== null) {
    return;
  }
  logUpdateTimeout = setTimeout(renderLog, LOG_RENDER_INTERVAL_MS);
}

function renderLog() {
  if (logUpdateTimeout !== null) {
    clearTimeout(logUpdateTimeout);
    logUpdateTimeout = null;
  }

  logArea.textContent = logLines.join("\n");
  updateLogCounter();

  if (autoScrollEnabled) {
    logArea.scrollTop = logArea.scrollHeight;
  }
}

function clearLog() {
  logLines = [];
  logCharacterCount = 0;
  renderLog();
}

function updateLogCounter() {
  logCounter.textContent = formatLogCounter(logLines.length);
}

function setSystemStatus(text, busy = false) {
  systemStatus.classList.toggle("busy", busy);
  systemStatus.querySelector("span:last-child").textContent = text;
}

// Update flash progress.
function updateProgress(progress, message) {
  progressBar.style.width = `${progress}%`;
  const text = message?.key ? t(message.key, message.values) : String(message || `${progress}%`);
  progressText.textContent = text;
  if (message && progress !== lastProgressLog) {
    lastProgressLog = progress;
    log(text);
  }
}

// Show or hide flash progress.
function showProgress(show) {
  progressContainer.hidden = !show;
  if (!show) {
    progressBar.style.width = "0%";
    progressText.textContent = "0%";
    lastProgressLog = null;
  }
}

// Subscribe to flash progress events.
EventsOn("flash-progress", (data) => {
  updateProgress(data.progress, data.message);
});

EventsOn("flash-log", (message) => {
  log(message);
});

// Subscribe to serial monitor events.
EventsOn("monitor-data", (data) => {
  if (!isMonitoring) {
    return;
  }
  const timestamp = formatTime(new Date());
  const lines = String(data)
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => `[${timestamp}] ${line}`);
  appendLogLines(lines);
});

EventsOn("monitor-error", (error) => {
  log(t("log.monitorError", { error }));
  stopMonitoring();
});

EventsOn("monitor-stop", () => {
  stopMonitoring();
});

// This check is deliberately non-blocking: a failed network request must never
// get in the way of flashing a device.
async function checkForUpdate() {
  try {
    const update = await CheckForUpdate();
    if (!update?.available) {
      return;
    }
    availableUpdate = update;
    btnUpdate.hidden = false;
    btnUpdate.querySelector("span:last-child").textContent = t("update.version", { version: update.version });
    if (!update.canInstall) {
      btnUpdate.title = t("update.requiresInstalledApp");
      btnUpdate.setAttribute("aria-label", t("update.availableAria"));
    }
  } catch (error) {
    log(t("log.updateCheckUnavailable", { error }));
  }
}

async function showAppVersion() {
  try {
    const version = await GetAppVersion();
    appVersionTag.textContent = version.startsWith("v") ? version : `v${version}`;
  } catch (error) {
    appVersionTag.hidden = true;
    log(t("log.versionUnavailable", { error }));
  }
}

btnUpdate.addEventListener("click", async () => {
  if (!availableUpdate) {
    return;
  }
  if (isMonitoring || isFlashing) {
    alert(t("alert.stopBeforeUpdate"));
    return;
  }
  if (!availableUpdate.canInstall) {
    btnUpdate.querySelector("span:last-child").textContent = t("update.unavailable");
    btnUpdate.title = t("update.writableLocation");
    setSystemStatus(t("status.updateUnavailable"));
    log(t("log.updateRequiresInstalledApp"));
    return;
  }

  btnUpdate.disabled = true;
  btnFlash.disabled = true;
  btnMonitor.disabled = true;
  btnUpdate.querySelector("span:last-child").textContent = t("update.downloading");
  setSystemStatus(t("status.updating"), true);
  try {
    await InstallUpdate();
  } catch (error) {
    const message = String(error);
    btnUpdate.disabled = false;
    btnFlash.disabled = false;
    btnMonitor.disabled = false;
    btnUpdate.querySelector("span:last-child").textContent = t("update.retry");
    btnUpdate.title = message;
    setSystemStatus(t("status.updateFailed"));
    log(t("log.updateFailed", { error: message }));
  }
});

// Discover and display serial ports.
async function refreshPorts() {
  portSelect.innerHTML = "";
  try {
    const ports = await ListPorts();
    ports.forEach((p) => {
      const o = document.createElement("option");
      o.value = p;
      o.textContent = p;
      portSelect.appendChild(o);
    });
    log(t("log.portsFound", { count: ports.length }));
  } catch (e) {
    log(t("log.listPortsError", { error: e }));
  }
}

// Select a firmware image.
btnChoose.addEventListener("click", async () => {
  try {
    const res = await ChooseFile(language);
    if (res) {
      filePath.value = res;
      log(t("log.selectedFile", { file: res }));
    }
  } catch (e) {
    log(t("log.fileSelectionError", { error: e }));
  }
});

// Flash button.
btnFlash.addEventListener("click", async () => {
  const port = portSelect.value;
  const file = filePath.value;
  if (!port || !file) {
    alert(t("alert.selectPortAndFile"));
    return;
  }

  if (isMonitoring) {
    alert(t("alert.stopMonitorBeforeFlash"));
    return;
  }

  // Lock conflicting controls during flashing.
  isFlashing = true;
  btnFlash.disabled = true;
  btnChoose.disabled = true;
  btnRefresh.disabled = true;
  btnMonitor.disabled = true;
  portSelect.disabled = true;
  baudSelect.disabled = true;
  setSystemStatus(t("status.flashing"), true);

  // Clear the log and reveal progress.
  clearLog();
  showProgress(true);

  log(t("log.startFlash", { file, port }));

  try {
    await Flash(port, file);
    log(t("log.flashSuccess"));
    setTimeout(() => {
      alert(t("alert.flashSuccess"));
    }, 100);
  } catch (e) {
    log(t("log.flashError", { error: e }));
    updateProgress(0, { key: "progress.failed" });
    setTimeout(() => {
      alert(t("alert.flashError", { error: e }));
    }, 100);
  } finally {
    // Restore the controls and hide progress.
    setTimeout(() => {
      showProgress(false);
      btnFlash.disabled = false;
      btnChoose.disabled = false;
      btnRefresh.disabled = false;
      btnMonitor.disabled = false;
      portSelect.disabled = false;
      baudSelect.disabled = false;
      setSystemStatus(t("status.ready"));
      isFlashing = false;
    }, 1000); // Leave the final state visible briefly.
  }
});

// Serial monitor button.
btnMonitor.addEventListener("click", async () => {
  const port = portSelect.value;
  const baud = parseInt(baudSelect.value);
  if (!port) {
    alert(t("alert.selectPortToMonitor"));
    return;
  }

  try {
    // Clear old output before monitoring starts.
    clearLog();

    startMonitoring();
    await MonitorPort(port, baud);
    log(t("log.monitoring", { port, baud }));
  } catch (e) {
    stopMonitoring();
    log(t("log.monitorStartError", { error: e }));
    alert(t("alert.monitorStartError", { error: e }));
  }
});

// Stop-monitor button.
btnStopMonitor.addEventListener("click", async () => {
  stopMonitoring();
  try {
    await StopMonitor();
  } catch (e) {
    log(t("log.monitorStopError", { error: e }));
  }
});

// Serial monitor UI state.
function startMonitoring() {
  isMonitoring = true;
  btnMonitor.hidden = true;
  btnStopMonitor.hidden = false;
  btnFlash.disabled = true;
  portSelect.disabled = true;
  baudSelect.disabled = true;
  btnUpdate.disabled = true;
  setSystemStatus(t("status.monitoring"), true);
}

function stopMonitoring() {
  isMonitoring = false;
  btnMonitor.hidden = false;
  btnStopMonitor.hidden = true;
  btnFlash.disabled = false;
  portSelect.disabled = false;
  baudSelect.disabled = false;
  btnUpdate.disabled = false;
  setSystemStatus(t("status.ready"));

  renderLog();
}

// Clear-log button.
btnClearLog.addEventListener("click", () => {
  clearLog();
  log(t("log.cleared"));
});

// Auto-scroll button.
btnAutoScroll.addEventListener("click", () => {
  autoScrollEnabled = !autoScrollEnabled;

  if (autoScrollEnabled) {
    btnAutoScroll.classList.add("active");
    btnAutoScroll.setAttribute("aria-pressed", "true");
    // Jump to the newest line when re-enabled.
    requestAnimationFrame(() => {
      logArea.scrollTop = logArea.scrollHeight;
    });
    log(t("log.autoScrollEnabled"));
  } else {
    btnAutoScroll.classList.remove("active");
    btnAutoScroll.setAttribute("aria-pressed", "false");
    log(t("log.autoScrollDisabled"));
  }
});

// Initial startup.
btnRefresh.addEventListener("click", refreshPorts);

// Keyboard shortcut shown on the primary action.
document.addEventListener("keydown", (event) => {
  if (event.key === "Enter" && !event.repeat && !btnFlash.disabled) {
    const interactiveTarget = event.target instanceof Element
      ? event.target.closest("button, select")
      : null;
    if (!interactiveTarget) {
      btnFlash.click();
    }
  }
});

translateDocument();

// Initialize the auto-scroll button state.
if (autoScrollEnabled) {
  btnAutoScroll.classList.add("active");
  btnAutoScroll.setAttribute("aria-pressed", "true");
} else {
  btnAutoScroll.classList.remove("active");
  btnAutoScroll.setAttribute("aria-pressed", "false");
}

updateLogCounter();
refreshPorts();
showAppVersion();
checkForUpdate();
