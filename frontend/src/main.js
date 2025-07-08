import { ListPorts, Flash, ChooseFile } from "../wailsjs/go/main/App.js";
import { EventsOn } from "../wailsjs/runtime/runtime.js";

const portSelect = document.getElementById("portSelect");
const btnRefresh = document.getElementById("btnRefresh");
const btnChoose = document.getElementById("btnChoose");
const btnFlash = document.getElementById("btnFlash");
const filePath = document.getElementById("filePath");
const logArea = document.getElementById("log");
const progressContainer = document.getElementById("progressContainer");
const progressBar = document.getElementById("progressBar");
const progressText = document.getElementById("progressText");

// Залить лог
function log(msg) {
  const timestamp = new Date().toLocaleTimeString();
  logArea.textContent += `[${timestamp}] ${msg}\n`;
  
  // Автоскролл к последнему сообщению
  logArea.scrollTop = logArea.scrollHeight;
  
  // Ограничиваем количество строк в логе (последние 1000 строк)
  const lines = logArea.textContent.split('\n');
  if (lines.length > 1000) {
    logArea.textContent = lines.slice(-1000).join('\n');
  }
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
  progressContainer.style.display = show ? 'block' : 'none';
  if (!show) {
    progressBar.style.width = '0%';
    progressText.textContent = '0%';
  }
}

// Настройка событий для прогресса
EventsOn("flash-progress", (data) => {
  updateProgress(data.progress, data.message);
});

EventsOn("flash-log", (message) => {
  log(message);
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
  if (!port || !file) {
    alert("Укажите порт и файл!");
    return;
  }

  // Блокируем интерфейс
  btnFlash.disabled = true;
  btnChoose.disabled = true;
  btnRefresh.disabled = true;
  portSelect.disabled = true;

  // Очищаем лог и показываем прогресс
  logArea.textContent = "";
  showProgress(true);

  log(`🚀 Начинаем прошивку ${file} → ${port}`);
  
  try {
    await Flash(port, file);
    log("✅ Прошивка успешно завершена!");
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
      portSelect.disabled = false;
    }, 1000); // Задержка, чтобы пользователь увидел финальное состояние
  }
});

// При старте
btnRefresh.addEventListener("click", refreshPorts);
refreshPorts();
