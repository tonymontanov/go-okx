/*
ФАЙЛ: types/cancel-all-after.go

ОПИСАНИЕ:
CancelAllAfterResult — ответ OKX на POST /api/v5/trade/cancel-all-after.

СЕМАНТИКА (OKX dead-man's switch):
Endpoint вооружает таймер на стороне биржи: если в течение `timeout`
секунд клиент не дёрнет endpoint снова, биржа автоматически отменит ВСЕ
открытые ордера данной учётки.

Это safety-механизм для риск-менеджмента: «если мой процесс упал /
сеть пропала / я завис — пусть биржа сама закроет всё, чем я выставил».

Способы использования:
  - timeout > 0 (10..120с): арм таймера. triggerTime в ответе — время,
    когда биржа отменит ордера, если не получит refresh;
  - timeout == 0: disarm. triggerTime в ответе будет пустой.

КАК ПОДДЕРЖИВАТЬ ТАЙМЕР:
В hot loop стратегии вызывайте CancelAllAfter каждые ~⅓ от timeout
(например, для timeout=30s — каждые 10s). Это даёт большой запас на
сетевые задержки.

ПАРНОСТЬ С MASSCANCEL:
mass-cancel (Phase 2.1) — синхронный panic-button «отмени всё сейчас».
cancel-all-after — асинхронный «отмени всё через N секунд, если я не
обновлю». Используются совместно: arm на старте, mass-cancel или
disarm на graceful shutdown.
*/

package types

// CancelAllAfterResult — ответ биржи на cancel-all-after.
type CancelAllAfterResult struct {
	// TriggerTimeMs — момент в мс, когда биржа применит cancel-all,
	// если не получит refresh. 0 для disarm (timeout=0).
	TriggerTimeMs int64
	// TsMs — таймштамп ответа сервера в мс.
	TsMs int64
}
