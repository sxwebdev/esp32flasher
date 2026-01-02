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
const offsetSelect = document.getElementById("offsetSelect");
const customOffset = document.getElementById("customOffset");
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

// Показ/скрытие поля для custom offset
offsetSelect.addEventListener("change", () => {
  if (offsetSelect.value === "custom") {
    customOffset.style.display = "block";
    customOffset.focus();
  } else {
    customOffset.style.display = "none";
  }
});

// Получить текущий offset
function getFlashOffset() {
  if (offsetSelect.value === "custom") {
    const val = customOffset.value.trim();
    if (val.startsWith("0x") || val.startsWith("0X")) {
      return parseInt(val, 16);
    }
    return parseInt(val, 10);
  }
  return parseInt(offsetSelect.value, 16);
}

let isMonitoring = false;
let autoScrollEnabled = true;
let logLines = [];
const MAX_LOG_LINES = 300;

// Throttling для обновления DOM
let pendingLines = [];
let updateScheduled = false;
let lastUpdateTime = 0;
const MIN_UPDATE_INTERVAL = 100; // Минимум 100мс между обновлениями

// Запланировать обновление DOM с throttling
function scheduleUpdate() {
  if (updateScheduled) return;

  const now = Date.now();
  const timeSinceLastUpdate = now - lastUpdateTime;

  if (timeSinceLastUpdate < MIN_UPDATE_INTERVAL) {
    // Откладываем обновление
    updateScheduled = true;
    setTimeout(() => {
      updateScheduled = false;
      flushPendingLines();
    }, MIN_UPDATE_INTERVAL - timeSinceLastUpdate);
  } else {
    flushPendingLines();
  }
}

// Применить накопленные строки к DOM
function flushPendingLines() {
  if (pendingLines.length === 0) return;

  lastUpdateTime = Date.now();

  // Добавляем накопленные строки
  logLines.push(...pendingLines);
  pendingLines = [];

  // Ограничиваем количество строк
  if (logLines.length > MAX_LOG_LINES) {
    logLines = logLines.slice(-MAX_LOG_LINES);
  }

  // Обновляем DOM один раз
  logArea.textContent = logLines.join("\n");

  // Автоскролл
  if (autoScrollEnabled) {
    logArea.scrollTop = logArea.scrollHeight;
  }
}

// Форматирование времени с миллисекундами
function formatTime(date) {
  const h = String(date.getHours()).padStart(2, '0');
  const m = String(date.getMinutes()).padStart(2, '0');
  const s = String(date.getSeconds()).padStart(2, '0');
  const ms = String(date.getMilliseconds()).padStart(3, '0');
  return `${h}:${m}:${s}.${ms}`;
}

// Залить лог
function log(msg) {
  const timestamp = formatTime(new Date());
  addLogLine(`[${timestamp}] ${msg}`);
}

// Эффективное добавление строки в лог
function addLogLine(line) {
  pendingLines.push(line);
  scheduleUpdate();
}

// Обновить прогресс
function updateProgress(progress, message) {
  progressBar.style.width = `${progress}%`;
  progressText.textContent = `${progress}%`;
  if (message) {
    log(message);
  }
}

// Показать/скрыть прогресс
function showProgress(show) {
  progressContainer.style.display = show ? "block" : "none";
  if (!show) {
    progressBar.style.width = "0%";
    progressText.textContent = "0%";
  }
}

// Настройка событий для прогресса
EventsOn("flash-progress", (data) => {
  updateProgress(data.progress, data.message);
});

EventsOn("flash-log", (message) => {
  log(message);
});

// События мониторинга порта
EventsOn("monitor-data", (data) => {
  if (data.trim()) {
    const timestamp = formatTime(new Date());
    addLogLine(`[${timestamp}] ${data.trim()}`);
  }
});

EventsOn("monitor-error", (error) => {
  log(`❌ Ошибка мониторинга: ${error}`);
  stopMonitoring();
});

EventsOn("monitor-stop", () => {
  stopMonitoring();
});

// Получить и показать порты
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
    log(`Найдено портов: ${ports.length}`);
  } catch (e) {
    log("Ошибка ListPorts: " + e);
  }
}

// Выбор файла
btnChoose.addEventListener("click", async () => {
  try {
    const res = await ChooseFile();
    if (res) {
      filePath.value = res;
      log("Выбран " + res);
    }
  } catch (e) {
    log("Ошибка выбора файла: " + e);
  }
});

// Кнопка «Прошить»
btnFlash.addEventListener("click", async () => {
  const port = portSelect.value;
  const file = filePath.value;
  const offset = getFlashOffset();

  if (!port || !file) {
    alert("Укажите порт и файл!");
    return;
  }

  if (isNaN(offset) || offset < 0) {
    alert("Некорректный адрес прошивки!");
    return;
  }

  if (isMonitoring) {
    alert("Остановите мониторинг перед прошивкой!");
    return;
  }

  // Блокируем интерфейс
  btnFlash.disabled = true;
  btnChoose.disabled = true;
  btnRefresh.disabled = true;
  btnMonitor.disabled = true;
  portSelect.disabled = true;
  baudSelect.disabled = true;
  offsetSelect.disabled = true;
  customOffset.disabled = true;

  // Очищаем лог и показываем прогресс
  logArea.textContent = "";
  logLines = [];
  showProgress(true);

  log(`🚀 Начинаем прошивку ${file} → ${port} @ 0x${offset.toString(16).toUpperCase()}`);

  try {
    await Flash(port, file, offset);
    setTimeout(() => {
      alert("Прошивка завершена успешно!");
    }, 100);
  } catch (e) {
    log("❌ Ошибка прошивки: " + e);
    updateProgress(0, "Ошибка");
    setTimeout(() => {
      alert("Ошибка прошивки: " + e);
    }, 100);
  } finally {
    // Разблокируем интерфейс и скрываем прогресс
    setTimeout(() => {
      showProgress(false);
      btnFlash.disabled = false;
      btnChoose.disabled = false;
      btnRefresh.disabled = false;
      btnMonitor.disabled = false;
      portSelect.disabled = false;
      baudSelect.disabled = false;
      offsetSelect.disabled = false;
      customOffset.disabled = false;
    }, 1000); // Задержка, чтобы пользователь увидел финальное состояние
  }
});

// Кнопка мониторинга порта
btnMonitor.addEventListener("click", async () => {
  const port = portSelect.value;
  const baud = parseInt(baudSelect.value);
  if (!port) {
    alert("Выберите COM-порт для мониторинга!");
    return;
  }

  try {
    // Очищаем лог перед началом мониторинга
    logArea.textContent = "";

    await MonitorPort(port, baud);
    startMonitoring();
    log(`🔍 Мониторинг порта ${port} запущен (${baud} baud)`);
  } catch (e) {
    log("❌ Ошибка запуска мониторинга: " + e);
    alert("Ошибка запуска мониторинга: " + e);
  }
});

// Кнопка остановки мониторинга
btnStopMonitor.addEventListener("click", async () => {
  try {
    await StopMonitor();
    stopMonitoring();
  } catch (e) {
    log("❌ Ошибка остановки мониторинга: " + e);
  }
});

// Функции мониторинга
function startMonitoring() {
  isMonitoring = true;
  btnMonitor.style.display = "none";
  btnStopMonitor.style.display = "inline-block";
  btnFlash.disabled = true;
  portSelect.disabled = true;
  baudSelect.disabled = true;
}

function stopMonitoring() {
  isMonitoring = false;
  btnMonitor.style.display = "inline-block";
  btnStopMonitor.style.display = "none";
  btnFlash.disabled = false;
  portSelect.disabled = false;
  baudSelect.disabled = false;
}

// Кнопка очистки лога
btnClearLog.addEventListener("click", () => {
  logLines = [];
  pendingLines = [];
  logArea.textContent = "";
  log("🗑️ Лог очищен");
});

// Кнопка автоскролла
btnAutoScroll.addEventListener("click", () => {
  autoScrollEnabled = !autoScrollEnabled;

  if (autoScrollEnabled) {
    btnAutoScroll.classList.add("active");
    btnAutoScroll.textContent = "📜 Auto";
    requestAnimationFrame(() => {
      logArea.scrollTop = logArea.scrollHeight;
    });
  } else {
    btnAutoScroll.classList.remove("active");
    btnAutoScroll.textContent = "📜 Auto";
  }
});

// При старте
btnRefresh.addEventListener("click", refreshPorts);

// Инициализируем состояние кнопки автоскролла
if (autoScrollEnabled) {
  btnAutoScroll.classList.add("active");
  btnAutoScroll.textContent = "📜 Auto";
} else {
  btnAutoScroll.classList.remove("active");
  btnAutoScroll.textContent = "📜 Auto";
}

refreshPorts();
