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
  const timestamp = new Date().toLocaleTimeString();
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
      line = `${line.slice(0, MAX_LOG_LINE_CHARACTERS)} … [truncated]`;
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
  const count = logLines.length;
  const suffix = count === 1 ? "line" : "lines";
  logCounter.textContent = `${count} ${suffix}`;
}

function setSystemStatus(text, busy = false) {
  systemStatus.classList.toggle("busy", busy);
  systemStatus.querySelector("span:last-child").textContent = text;
}

// Update flash progress.
function updateProgress(progress, message) {
  progressBar.style.width = `${progress}%`;
  progressText.textContent = `${progress}%`;
  if (message && progress !== lastProgressLog) {
    lastProgressLog = progress;
    log(message);
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
  const timestamp = new Date().toLocaleTimeString();
  const lines = String(data)
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => `[${timestamp}] ${line}`);
  appendLogLines(lines);
});

EventsOn("monitor-error", (error) => {
  log(`❌ Monitor error: ${error}`);
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
    btnUpdate.querySelector("span:last-child").textContent = `Update ${update.version}`;
    if (!update.canInstall) {
      btnUpdate.title = "Automatic updates require the installed application";
      btnUpdate.setAttribute("aria-label", "Update available; install the application to update automatically");
    }
  } catch (error) {
    log(`Update check unavailable: ${error}`);
  }
}

async function showAppVersion() {
  try {
    const version = await GetAppVersion();
    appVersionTag.textContent = version.startsWith("v") ? version : `v${version}`;
  } catch (error) {
    appVersionTag.hidden = true;
    log(`Could not read app version: ${error}`);
  }
}

btnUpdate.addEventListener("click", async () => {
  if (!availableUpdate) {
    return;
  }
  if (isMonitoring || isFlashing) {
    alert("Stop monitoring or wait for flashing to finish before updating.");
    return;
  }
  if (!availableUpdate.canInstall) {
    btnUpdate.querySelector("span:last-child").textContent = "Automatic update unavailable";
    btnUpdate.title = "The application must be installed in a writable location";
    setSystemStatus("Update unavailable");
    log("Automatic updates require the installed application in a writable location.");
    return;
  }

  btnUpdate.disabled = true;
  btnFlash.disabled = true;
  btnMonitor.disabled = true;
  btnUpdate.querySelector("span:last-child").textContent = "Downloading update…";
  setSystemStatus("Updating", true);
  try {
    await InstallUpdate();
  } catch (error) {
    const message = String(error);
    btnUpdate.disabled = false;
    btnFlash.disabled = false;
    btnMonitor.disabled = false;
    btnUpdate.querySelector("span:last-child").textContent = "Update failed — retry";
    btnUpdate.title = message;
    setSystemStatus("Update failed");
    log(`Update failed: ${message}`);
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
    log(`Found ${ports.length} ports`);
  } catch (e) {
    log("ListPorts error: " + e);
  }
}

// Select a firmware image.
btnChoose.addEventListener("click", async () => {
  try {
    const res = await ChooseFile();
    if (res) {
      filePath.value = res;
      log("Selected " + res);
    }
  } catch (e) {
    log("File selection error: " + e);
  }
});

// Flash button.
btnFlash.addEventListener("click", async () => {
  const port = portSelect.value;
  const file = filePath.value;
  if (!port || !file) {
    alert("Select a port and firmware file.");
    return;
  }

  if (isMonitoring) {
    alert("Stop serial monitoring before flashing.");
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
  setSystemStatus("Flashing", true);

  // Clear the log and reveal progress.
  clearLog();
  showProgress(true);

  log(`🚀 Starting flash ${file} → ${port}`);

  try {
    await Flash(port, file);
    log("✅ Firmware flashed successfully!");
    setTimeout(() => {
      alert("Firmware flashed successfully!");
    }, 100);
  } catch (e) {
    log("❌ Flash error: " + e);
    updateProgress(0, "Error");
    setTimeout(() => {
      alert("Flash error: " + e);
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
      setSystemStatus("System ready");
      isFlashing = false;
    }, 1000); // Leave the final state visible briefly.
  }
});

// Serial monitor button.
btnMonitor.addEventListener("click", async () => {
  const port = portSelect.value;
  const baud = parseInt(baudSelect.value);
  if (!port) {
    alert("Select a serial port to monitor.");
    return;
  }

  try {
    // Clear old output before monitoring starts.
    clearLog();

    startMonitoring();
    await MonitorPort(port, baud);
    log(`🔍 Monitoring ${port} at ${baud} baud`);
  } catch (e) {
    stopMonitoring();
    log("❌ Could not start monitoring: " + e);
    alert("Could not start monitoring: " + e);
  }
});

// Stop-monitor button.
btnStopMonitor.addEventListener("click", async () => {
  stopMonitoring();
  try {
    await StopMonitor();
  } catch (e) {
    log("❌ Could not stop monitoring: " + e);
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
  setSystemStatus("Monitoring", true);
}

function stopMonitoring() {
  isMonitoring = false;
  btnMonitor.hidden = false;
  btnStopMonitor.hidden = true;
  btnFlash.disabled = false;
  portSelect.disabled = false;
  baudSelect.disabled = false;
  btnUpdate.disabled = false;
  setSystemStatus("System ready");

  renderLog();
}

// Clear-log button.
btnClearLog.addEventListener("click", () => {
  clearLog();
  log("🗑️ Log cleared");
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
    log("📜 Auto-scroll enabled");
  } else {
    btnAutoScroll.classList.remove("active");
    btnAutoScroll.setAttribute("aria-pressed", "false");
    log("⏸️ Auto-scroll disabled");
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
