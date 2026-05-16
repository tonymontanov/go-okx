/*
ФАЙЛ: types/enums.go

ОПИСАНИЕ:
Протокольные enum'ы OKX v5, общие для spot и swap (и будущих профилей).
Все значения — string-typed enum'ы, точно совпадающие с протоколом OKX v5
(https://www.okx.com/docs-v5/en/). Это позволяет:
  - сразу сериализовать/десериализовать без маппинга;
  - использовать значения напрямую в URL/JSON;
  - сравнивать через `==` без аллокаций.

ЧТО ВЫНЕСЕНО ИЗ swap/types/enums.go:
  - SideType        — buy/sell.
  - OrderType       — общие значения (market/limit/post_only/fok/ioc).
                      OrderTypeOptimalLimitIOC остаётся в swap/types как
                      профильно-специфичный (используется только для
                      ClosePosition по рынку).
  - TimeInForceType — GTC/IOC/FOK/PostOnly (Binance-style; маппится в
                      OKX OrderType в SDK).
  - TdMode          — cross/isolated/cash. Все три значения протокольные;
                      cash — spot default, cross/isolated — swap default.
  - InstType        — SPOT/SWAP/FUTURES/OPTION.
  - OrderState      — live/partially_filled/filled/canceled/unknown + ParseOrderState.

ЧТО ОСТАЛОСЬ В swap/types/enums.go (профильное):
  - PosSide        — long/short/net (стороны позиции в hedge-mode SWAP).
  - PositionMode   — net_mode/long_short_mode (режим аккаунта для SWAP).
  - OrderTypeOptimalLimitIOC — swap-only константа над общим OrderType.
*/

package types

// SideType — направление ордера. Общее для всех профилей OKX.
type SideType string

const (
	// SideTypeBuy — покупка.
	SideTypeBuy SideType = "buy"
	// SideTypeSell — продажа.
	SideTypeSell SideType = "sell"
)

// OrderType — тип ордера в нотации OKX. POST_ONLY / FOK / IOC у OKX выражены
// отдельным OrderType, а не через TIF — это особенность их REST-протокола.
//
// Этот enum несёт ОБЩИЕ значения, поддерживаемые spot и swap. Профильно-
// специфичные значения (например, OrderTypeOptimalLimitIOC у swap) объявляются
// в своём пакете как константы того же OrderType.
type OrderType string

const (
	// OrderTypeMarket — рыночный ордер.
	OrderTypeMarket OrderType = "market"
	// OrderTypeLimit — лимитный (GTC).
	OrderTypeLimit OrderType = "limit"
	// OrderTypePostOnly — лимитный post-only (аналог TIF=GTX у Binance).
	OrderTypePostOnly OrderType = "post_only"
	// OrderTypeFOK — fill-or-kill.
	OrderTypeFOK OrderType = "fok"
	// OrderTypeIOC — immediate-or-cancel.
	OrderTypeIOC OrderType = "ioc"
)

// TimeInForceType — TIF в нотации core/types (Binance-style). Для отправки в
// OKX он маппится в соответствующий OrderType.
type TimeInForceType string

const (
	// TimeInForceTypeGTC — Good Till Cancel (лимит).
	TimeInForceTypeGTC TimeInForceType = "GTC"
	// TimeInForceTypeIOC — Immediate or Cancel.
	TimeInForceTypeIOC TimeInForceType = "IOC"
	// TimeInForceTypeFOK — Fill or Kill.
	TimeInForceTypeFOK TimeInForceType = "FOK"
	// TimeInForceTypeGTX — Post Only.
	TimeInForceTypeGTX TimeInForceType = "GTX"
)

// TdMode — margin-mode ордера/позиции в OKX.
// Все три значения — протокольные константы OKX:
//   - cash      используется по умолчанию для SPOT;
//   - cross     по умолчанию для SWAP (cross-margin);
//   - isolated  для isolated-маржинальных позиций.
type TdMode string

const (
	// TdModeCross — cross-margin.
	TdModeCross TdMode = "cross"
	// TdModeIsolated — isolated margin.
	TdModeIsolated TdMode = "isolated"
	// TdModeCash — cash trading (без плеча); SPOT по умолчанию.
	TdModeCash TdMode = "cash"
)

// InstType — тип инструмента OKX.
type InstType string

const (
	// InstTypeSpot — спотовый инструмент.
	InstTypeSpot InstType = "SPOT"
	// InstTypeSWAP — USD-M Perpetual SWAP.
	InstTypeSWAP InstType = "SWAP"
	// InstTypeFutures — фьючерс с экспирацией.
	InstTypeFutures InstType = "FUTURES"
	// InstTypeOption — опцион.
	InstTypeOption InstType = "OPTION"
)

// OrderState — минимальный enum статуса ордера. Введён как компромисс между
// "не вводить общую модель статусов" и реальной потребностью фильтровать
// live/filled/canceled.
type OrderState string

const (
	// OrderStateLive — активен, ожидает исполнения.
	OrderStateLive OrderState = "live"
	// OrderStatePartiallyFilled — частично исполнен, остаток активен.
	OrderStatePartiallyFilled OrderState = "partially_filled"
	// OrderStateFilled — полностью исполнен.
	OrderStateFilled OrderState = "filled"
	// OrderStateCanceled — отменён.
	OrderStateCanceled OrderState = "canceled"
	// OrderStateUnknown — не удалось распарсить статус (защитный fallback).
	OrderStateUnknown OrderState = "unknown"
)

// ParseOrderState парсит строковый статус ордера OKX в типизированный enum.
// OKX возможные значения: live, partially_filled, filled, canceled, mmp_canceled.
// Всё неизвестное → OrderStateUnknown (диагностика — через лог вызывающим кодом).
func ParseOrderState(s string) OrderState {
	switch s {
	case "live":
		return OrderStateLive
	case "partially_filled":
		return OrderStatePartiallyFilled
	case "filled":
		return OrderStateFilled
	case "canceled", "mmp_canceled":
		return OrderStateCanceled
	default:
		return OrderStateUnknown
	}
}
