/*
ФАЙЛ: rate-limit-event.go

ОПИСАНИЕ:
Публичный тип RateLimitEvent, который SDK передаёт подписчикам через
okx.Config.RateLimitEventObserver. Добавлен в v2.2.0 как замена/расширение
старого RateLimitObserver (см. config.go).

ЗАЧЕМ:
OKX rate-limit модель имеет три измерения, которые НЕ могут быть выведены
только из (endpoint, headers):

 1. Единица учёта на batch-эндпоинтах — ORDER, а не REQUEST. Один POST
    /api/v5/trade/batch-orders на 20 ордеров стоит 20 единиц из бюджета
    "300 orders per 2s", а не 1 единицу из несуществующего "300 requests
    per 2s". Без OrderCount внешний счётчик занижает usage в 1-20x.
 2. Trading-лимиты per (User ID + Instrument ID), а не глобально per-UID.
    У каждого символа свой бюджет. Без Symbols подписчик вынужден
    агрегировать по endpoint'у и блокировать один символ из-за нагрузки
    на другой.
 3. Sub-account-level лимит "1000 new+amend orders / 2s" (error 50061)
    считает только POST в категориях Place и Amend, не Cancel и не Query.
    Без Category подписчик не может построить эту плоскость.

Этот тип — единственный официальный источник истины для всех трёх
измерений. SDK заполняет его на стороне доменных методов (swap/trading.go,
swap/account.go), где известны и instId-ы, и реальное число ордеров в теле
запроса.

ЗАВИСИМОСТИ:
Никаких — это plain data struct.
*/

package okx

// RateLimitCategory — классификация REST-вызова с точки зрения OKX
// rate-limit модели. Используется внешними rate-limiter'ами для разнесения
// usage'а по разным плоскостям лимитов (per-endpoint, per-symbol,
// sub-account-level).
type RateLimitCategory string

const (
	// RateLimitCategoryPlace — создание ордера(ов).
	// Endpoints: /api/v5/trade/order, /api/v5/trade/batch-orders,
	// /api/v5/trade/close-position.
	// Учитывается в sub-account 1000 new+amend orders / 2s (error 50061).
	RateLimitCategoryPlace RateLimitCategory = "place"

	// RateLimitCategoryAmend — изменение ордера(ов).
	// Endpoints: /api/v5/trade/amend-order, /api/v5/trade/amend-batch-orders.
	// Учитывается в sub-account 1000 new+amend orders / 2s.
	RateLimitCategoryAmend RateLimitCategory = "amend"

	// RateLimitCategoryCancel — отмена ордера(ов).
	// Endpoints: /api/v5/trade/cancel-order, /api/v5/trade/cancel-batch-orders,
	// /api/v5/trade/cancel-all-after.
	// В sub-account 50061 НЕ учитывается (OKX считает только place + amend).
	RateLimitCategoryCancel RateLimitCategory = "cancel"

	// RateLimitCategoryQuery — приватный GET либо неторговый POST (account
	// configuration). Per-UID, в sub-account 50061 не входит.
	// Endpoints: /api/v5/trade/orders-pending, /api/v5/account/*.
	RateLimitCategoryQuery RateLimitCategory = "query"

	// RateLimitCategoryMarketData — публичный GET (per-IP лимиты).
	// Endpoints: /api/v5/market/*, /api/v5/public/*.
	RateLimitCategoryMarketData RateLimitCategory = "market"

	// RateLimitCategoryUnknown — fallback для запросов, не покрытых ни
	// одной из явных категорий (например, внутренние health-check'и).
	// Внешний rate-limiter может либо игнорировать такие события, либо
	// засчитывать их консервативно в Query.
	RateLimitCategoryUnknown RateLimitCategory = ""
)

// String возвращает строковое представление категории.
func (c RateLimitCategory) String() string {
	return string(c)
}

// RateLimitEvent — структурированное rate-limit событие, которое SDK
// передаёт подписчикам через okx.Config.RateLimitEventObserver.
//
// Все поля заполняются SDK строго после успешного получения HTTP-ответа
// от OKX (даже если код ответа != 0): observer вызывается ровно один раз
// на каждый завершённый REST-вызов.
type RateLimitEvent struct {
	// Endpoint — путь запроса (например, "/api/v5/trade/batch-orders").
	// Никогда не пустой. Этот же путь будет в OKX docs §Rate Limits.
	Endpoint string

	// Method — HTTP-метод запроса в верхнем регистре (GET / POST / ...).
	Method string

	// Headers — rate-limit заголовки, которые OKX вернул в ответе:
	// ratelimit-limit / ratelimit-remaining / ratelimit-reset и их
	// x-ratelimit-* варианты. На момент v2.2.0 OKX REST API эти заголовки
	// не возвращает — map будет пустым, но всегда non-nil. SDK уже
	// прокидывает их сразу, как только OKX начнёт отдавать.
	Headers map[string]string

	// OrderCount — сколько ордеров СОЗДАНО/ИЗМЕНЕНО/ОТМЕНЕНО этим запросом:
	//   - 1 для /trade/order, /trade/amend-order, /trade/cancel-order,
	//     /trade/close-position;
	//   - len(orders) для /trade/batch-orders, /trade/amend-batch-orders,
	//     /trade/cancel-batch-orders;
	//   - 0 для не-trading запросов (account, market, public).
	//
	// Это число выражает сколько единиц бюджета OKX списал/спишет с лимита
	// "300 orders per 2s" для соответствующего endpoint'а. Использовать
	// вместо счётчика запросов (которым раньше пользовался внешний
	// rate-limiter): на batch'ах он недосчитывает usage в 1-20x.
	OrderCount int

	// Symbols — отсортированный список уникальных OKX InstID, к которым
	// относится запрос:
	//   - 1 элемент для single trading method'ов (InstID ордера);
	//   - 1+ для batch (set unique InstID всех ордеров в батче);
	//   - 1 для query-методов с обязательным instId параметром
	//     (GetPositions, GetOpenOrders, GetSymbolInfo, ...);
	//   - пустой ([]string{}, не nil) для общих запросов без instId
	//     (GetBalance без ccy, public/instruments list).
	//
	// OKX trading лимиты per (UID + InstId), поэтому подписчик должен
	// списывать usage в state'ы соответствующих символов, а не агрегировать
	// по endpoint'у. Аккуратное использование этого поля устраняет
	// эффект "один горячий символ блокирует остальные".
	Symbols []string

	// Category — классификация по rate-limit модели OKX. Используется
	// внешним rate-limiter'ом для:
	//   - sub-account-level плоскости (Place + Amend = 1000 / 2s);
	//   - всегда-разрешать-Cancel политики;
	//   - правильного выбора окна / лимита для нестандартных endpoints.
	Category RateLimitCategory
}
