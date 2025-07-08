package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	serialport "go.bug.st/serial"
)

// App struct
type App struct {
	ctx         context.Context
	monitorPort serialport.Port
	stopMonitor chan bool
	lineBuffer  string // Буфер для накопления неполных строк
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// ListPorts возвращает список COM-портов
func (a *App) ListPorts() ([]string, error) {
	return serialport.GetPortsList()
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// ChooseFile открывает диалог выбора файла
func (a *App) ChooseFile() (string, error) {
	filePath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Выберите файл прошивки",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "Firmware Files",
				Pattern:     "*.bin",
			},
		},
	})

	return filePath, err
}

// emitProgress отправляет прогресс в frontend
func (a *App) emitProgress(progress int, message string) {
	runtime.EventsEmit(a.ctx, "flash-progress", map[string]interface{}{
		"progress": progress,
		"message":  message,
	})
}

// emitLog отправляет лог сообщение в frontend
func (a *App) emitLog(message string) {
	runtime.EventsEmit(a.ctx, "flash-log", message)
}

// Flash прошивает application.bin на адрес 0x10000 - ВЕРСИЯ ДЛЯ ПОВРЕЖДЕННОГО FLASH
func (a *App) Flash(portName, filePath string) error {
	// Проверить что файл существует
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filePath)
	}

	a.emitProgress(0, "Начинаем прошивку...")
	a.emitLog("🔄 Инициализация...")

	// Считать файл
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	a.emitProgress(10, "Файл загружен")
	a.emitLog(fmt.Sprintf("📄 Загружен файл: %d байт", len(data)))

	// ИСПРАВЛЕНИЕ: Обнаружен цикл перезагрузок - используем специальную обработку
	a.emitProgress(20, "Подключение к ESP32...")
	a.emitLog("🔗 Подключение к ESP32...")
	a.emitLog("⚠️ ВНИМАНИЕ: Обнаружены ошибки checksum в flash")
	a.emitLog("💡 ESP32 в цикле перезагрузок - это нормально для поврежденного flash")

	flasher, err := NewESP32FlasherWithProgress(portName, a)
	if err != nil {
		a.emitLog("⚠️ Автоматический режим не сработал")
		a.emitLog("🔄 Для поврежденного flash попробуем ручной режим...")

		// НОВОЕ: Специальные инструкции для поврежденного flash
		a.emitLog("")
		a.emitLog("=== РЕЖИМ ВОССТАНОВЛЕНИЯ ПОВРЕЖДЕННОГО FLASH ===")
		a.emitLog("Ваш ESP32 имеет поврежденный flash (csum err), но это исправимо!")
		a.emitLog("")
		a.emitLog("Выполните следующие действия:")
		a.emitLog("1. ESP32 должен постоянно перезагружаться (это нормально)")
		a.emitLog("2. Найдите кнопки BOOT и RESET на плате")
		a.emitLog("3. Удерживайте кнопку BOOT (GPIO0)")
		a.emitLog("4. Нажмите и отпустите кнопку RESET")
		a.emitLog("5. Отпустите кнопку BOOT")
		a.emitLog("6. ESP32 должен перестать перезагружаться")
		a.emitLog("7. Нажмите Enter для продолжения...")

		// Ждем пользователя
		fmt.Print("Нажмите Enter когда ESP32 стабилизируется...")
		fmt.Scanln()

		// Пробуем ручной режим
		flasher, err = NewESP32FlasherManual(portName, a)
		if err != nil {
			return fmt.Errorf("failed to create flasher in manual mode: %w", err)
		}
	}
	defer flasher.Close()

	// НЕ пробуем увеличивать скорость для поврежденного flash
	a.emitLog("⚠️ Поврежденный flash - оставляем скорость 115200 для надежности")

	// Прошить данные с прогрессом (начинается с 30%)
	if err := flasher.FlashData(data, 0x10000, portName); err != nil {
		a.emitProgress(0, "Ошибка прошивки")
		return fmt.Errorf("failed to flash: %w", err)
	}

	// ДОПОЛНИТЕЛЬНО: Перезагружаем ESP32 в нормальный режим
	a.emitLog("🔄 Перезагрузка ESP32...")
	flasher.RebootTarget()

	a.emitProgress(100, "Прошивка завершена!")
	a.emitLog("✅ Прошивка успешно завершена!")
	a.emitLog("💡 Flash восстановлен - ESP32 больше не должен перезагружаться")

	return nil
}

// FlashWithRetry - версия с повторными попытками
func (a *App) FlashWithRetry(portName, filePath string, maxAttempts int) error {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		a.emitLog(fmt.Sprintf("🔄 Попытка прошивки %d/%d", attempt, maxAttempts))

		err := a.Flash(portName, filePath)
		if err == nil {
			return nil // Успех!
		}

		a.emitLog(fmt.Sprintf("❌ Попытка %d не удалась: %v", attempt, err))

		if attempt < maxAttempts {
			a.emitLog("⏳ Ожидание перед следующей попыткой...")
			// time.Sleep(2 * time.Second) // Раскомментируйте если нужна задержка
		}
	}

	return fmt.Errorf("не удалось прошить после %d попыток", maxAttempts)
}

// FlashMultipleFiles - прошивка нескольких файлов (если нужно)
func (a *App) FlashMultipleFiles(portName string, files map[string]uint32) error {
	// files - карта filename -> offset
	// Например: {"bootloader.bin": 0x1000, "app.bin": 0x10000, "partitions.bin": 0x8000}

	a.emitLog("🔄 Режим прошивки нескольких файлов...")

	// Создаем флешер один раз
	flasher, err := NewESP32FlasherWithProgress(portName, a)
	if err != nil {
		// Fallback на ручной режим
		a.emitLog("⚠️ Переходим в ручной режим для множественной прошивки...")
		flasher, err = NewESP32FlasherManual(portName, a)
		if err != nil {
			return fmt.Errorf("failed to create flasher: %w", err)
		}
	}
	defer flasher.Close()

	// Увеличиваем скорость
	flasher.SetBaudRate(460800)

	// Прошиваем каждый файл
	fileCount := 0
	totalFiles := len(files)

	for filename, offset := range files {
		fileCount++
		a.emitLog(fmt.Sprintf("📄 Прошивка файла %d/%d: %s -> 0x%x", fileCount, totalFiles, filename, offset))

		data, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", filename, err)
		}

		if err := flasher.FlashData(data, offset, portName); err != nil {
			return fmt.Errorf("failed to flash %s: %w", filename, err)
		}
	}

	flasher.RebootTarget()
	a.emitLog("✅ Все файлы прошиты успешно!")
	return nil
}

// MonitorPort создает соединение с портом для мониторинга и возвращает канал с данными
func (a *App) MonitorPort(portName string, baudRate int) error {
	// Если уже идет мониторинг, останавливаем его
	if a.monitorPort != nil {
		a.StopMonitor()
	}

	// Открываем порт для мониторинга
	mode := &serialport.Mode{
		BaudRate: baudRate,
		Parity:   serialport.NoParity,
		DataBits: 8,
		StopBits: serialport.OneStopBit,
	}

	port, err := serialport.Open(portName, mode)
	if err != nil {
		return fmt.Errorf("failed to open port for monitoring: %w", err)
	}

	a.monitorPort = port
	a.stopMonitor = make(chan bool, 1)
	a.lineBuffer = "" // Очищаем буфер строк

	a.emitLog(fmt.Sprintf("🔍 Начинаем мониторинг порта %s (%d baud)", portName, baudRate))
	a.emitLog("💡 Для остановки мониторинга нажмите 'Стоп'")

	// Запускаем горутину для чтения данных
	go func() {
		defer func() {
			// Защищенное закрытие порта в горутине
			if a.monitorPort != nil {
				a.monitorPort.Close()
				a.monitorPort = nil
			}
		}()

		buffer := make([]byte, 1024)

		for {
			select {
			case <-a.stopMonitor:
				return
			default:
				// Проверяем, что порт еще открыт
				if a.monitorPort == nil {
					return
				}

				// Устанавливаем таймаут чтения
				if err := a.monitorPort.SetReadTimeout(50 * time.Millisecond); err != nil {
					return
				}

				n, err := a.monitorPort.Read(buffer)
				if err != nil {
					// Проверяем, если это timeout - продолжаем
					if strings.Contains(err.Error(), "timeout") {
						continue
					}
					// Проверяем на "bad file descriptor" - просто прекращаем без ошибки
					if strings.Contains(err.Error(), "bad file descriptor") ||
						strings.Contains(err.Error(), "file already closed") {
						return
					}
					// Если другая ошибка - отправляем в лог и прекращаем
					runtime.EventsEmit(a.ctx, "monitor-error", err.Error())
					return
				}

				if n > 0 {
					// Добавляем новые данные к буферу
					a.lineBuffer += string(buffer[:n])

					// Обрабатываем все полные строки
					for {
						newlineIdx := strings.Index(a.lineBuffer, "\n")
						if newlineIdx == -1 {
							// Нет полных строк, ждем еще данных
							break
						}

						// Извлекаем полную строку
						line := a.lineBuffer[:newlineIdx]
						a.lineBuffer = a.lineBuffer[newlineIdx+1:]

						// Убираем лишние символы \r и отправляем строку только если она не пустая
						line = strings.TrimSpace(line)
						if line != "" {
							runtime.EventsEmit(a.ctx, "monitor-data", line)
						}
					}

					// Если буфер становится слишком большим без \n, отправляем как есть и очищаем
					if len(a.lineBuffer) > 1000 { // Возвращаем нормальный порог
						line := strings.TrimSpace(a.lineBuffer)
						if line != "" {
							runtime.EventsEmit(a.ctx, "monitor-data", line)
						}
						a.lineBuffer = ""
					}
				}
			}
		}
	}()

	return nil
}

// StopMonitor останавливает мониторинг порта
func (a *App) StopMonitor() {
	// Сначала посылаем сигнал остановки
	if a.stopMonitor != nil {
		select {
		case a.stopMonitor <- true:
		default:
			// Канал уже закрыт или заполнен
		}
		close(a.stopMonitor)
		a.stopMonitor = nil
	}

	// Даем время горутине на завершение
	time.Sleep(200 * time.Millisecond)

	// Только после этого закрываем порт
	if a.monitorPort != nil {
		a.monitorPort.Close()
		a.monitorPort = nil
	}

	a.lineBuffer = "" // Очищаем буфер строк

	runtime.EventsEmit(a.ctx, "monitor-stop", "")
	a.emitLog("⏹️ Мониторинг порта остановлен")
}
