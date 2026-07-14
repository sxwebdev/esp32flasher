import {
  ListPorts,
  Flash,
  ChooseFile,
  MonitorPort,
  StopMonitor,
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

let isMonitoring = false;
let logUpdateTimeout = null; // Batches log rendering updates.
let autoScrollEnabled = true; // Auto-scroll is enabled by default.
let logLines = []; // In-memory terminal line buffer.
const MAX_LOG_LINES = 1000;

// Append a timestamped message to the log.
function log(msg) {
  const timestamp = new Date().toLocaleTimeString();
  addLogLine(`[${timestamp}] ${msg}`);
}

// Append one line to the terminal efficiently.
function addLogLine(line) {
  logLines.push(line);

  // Keep a bounded terminal-style scrollback buffer.
  if (logLines.length > MAX_LOG_LINES) {
    logLines = logLines.slice(-MAX_LOG_LINES);
  }

  // Render immediately.
  logArea.textContent = logLines.join("\n");
  updateLogCounter();

  // Follow the newest output when auto-scroll is enabled.
  if (autoScrollEnabled) {
    logArea.scrollTop = logArea.scrollHeight;
  }
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
  if (message) {
    log(message);
  }
}

// Show or hide flash progress.
function showProgress(show) {
  progressContainer.hidden = !show;
  if (!show) {
    progressBar.style.width = "0%";
    progressText.textContent = "0%";
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
  // Data already arrives as complete lines.
  if (data.trim()) {
    const timestamp = new Date().toLocaleTimeString();
    const logLine = `[${timestamp}] ${data.trim()}`;

    // Append the line and enforce the scrollback limit.
    logLines.push(logLine);
    if (logLines.length > MAX_LOG_LINES) {
      logLines = logLines.slice(-MAX_LOG_LINES);
    }

    // Render without batching for responsive serial output.
    logArea.textContent = logLines.join("\n");
    updateLogCounter();

    // Follow new output when enabled.
    if (autoScrollEnabled) {
      logArea.scrollTop = logArea.scrollHeight;
    }
  }
});

EventsOn("monitor-error", (error) => {
  log(`❌ Monitor error: ${error}`);
  stopMonitoring();
});

EventsOn("monitor-stop", () => {
  stopMonitoring();
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
  btnFlash.disabled = true;
  btnChoose.disabled = true;
  btnRefresh.disabled = true;
  btnMonitor.disabled = true;
  portSelect.disabled = true;
  baudSelect.disabled = true;
  setSystemStatus("Flashing", true);

  // Clear the log and reveal progress.
  logLines = [];
  logArea.textContent = "";
  updateLogCounter();
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
    logLines = [];
    logArea.textContent = "";
    updateLogCounter();

    await MonitorPort(port, baud);
    startMonitoring();
    log(`🔍 Monitoring ${port} at ${baud} baud`);
  } catch (e) {
    log("❌ Could not start monitoring: " + e);
    alert("Could not start monitoring: " + e);
  }
});

// Stop-monitor button.
btnStopMonitor.addEventListener("click", async () => {
  try {
    await StopMonitor();
    stopMonitoring();
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
  setSystemStatus("Monitoring", true);
}

function stopMonitoring() {
  isMonitoring = false;
  btnMonitor.hidden = false;
  btnStopMonitor.hidden = true;
  btnFlash.disabled = false;
  portSelect.disabled = false;
  baudSelect.disabled = false;
  setSystemStatus("System ready");

  // Cancel any pending log render.
  if (logUpdateTimeout) {
    clearTimeout(logUpdateTimeout);
    logUpdateTimeout = null;
  }
}

// Clear-log button.
btnClearLog.addEventListener("click", () => {
  logLines = [];
  logArea.textContent = "";
  updateLogCounter();
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
