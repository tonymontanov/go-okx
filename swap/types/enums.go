/*
ФАЙЛ: swap/types/enums.go

ОПИСАНИЕ:
Перечисления (enum-ы) для SWAP-профиля OKX. Все значения — string-typed enum'ы,
точно совпадающие с протоколом OKX v5 (https://www.okx.com/docs-v5/en/).
Это позволяет:
  - сразу сериализовать/десериализовать без маппинга;
  - использовать enum'ы напрямую в URL/JSON;
  - сделать значения сравнимыми через `==` без аллокаций.

ТИПЫ:
  - SideType        — buy/sell (направление ордера).
  - PosSide         — long/short/net (сторона позиции; для SWAP в hedge-режиме).
  - OrderType       — market/limit/post_only/fok/ioc/optimal_limit_ioc.
  - TimeInForceType — GTC/IOC/FOK/PostOnly (как в core/types/enums.go, для маппинга
    "сверху"). В сам REST OKX не отправляется — конвертируется в OrderType.
  - TdMode          — cross/isolated/cash (margin mode). Для SWAP допустимы только
    cross / isolated; cash — для SPOT-профиля. Хранится здесь, потому что SDK
    может выставлять tdMode по умолчанию для всего SWAP-клиента.
  - InstType        — SWAP/SPOT/FUTURES/OPTIONS. SWAP-профиль всегда отдаёт SWAP.
  - OrderState      — минимальный enum статуса ордера для совместимости с
    деском (см. ТЗ §5.4 и ответы на ТЗ).
*/

package types

// SideType — направление ордера.
type SideType string

const (
	// SideTypeBuy — покупка.
	SideTypeBuy SideType = "buy"
	// SideTypeSell — продажа.
	SideTypeSell SideType = "sell"
)

// PosSide — сторона позиции в SWAP. Используется в hedge-режиме (long_short_mode).
// В net_mode (по умолчанию для SDK) всегда передаётся "net".
type PosSide string

const (
	// PosSideLong — длинная сторона (hedge mode).
	PosSideLong PosSide = "long"
	// PosSideShort — короткая сторона (hedge mode).
	PosSideShort PosSide = "short"
	// PosSideNet — net-режим (одна позиция на инструмент).
	PosSideNet PosSide = "net"
)

// OrderType — тип ордера в нотации OKX. POST_ONLY / FOK / IOC у OKX выражены
// отдельным OrderType, а не через TIF — это особенность их REST-протокола.
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
	// OrderTypeOptimalLimitIOC — market-ордер для SWAP, исполняется по best
	// price + IOC. Используется в ClosePosition по рынку.
	OrderTypeOptimalLimitIOC OrderType = "optimal_limit_ioc"
)

// TimeInForceType — TIF в нотации core/types (Binance-style). Для отправки в
// OKX он маппится в соответствующий OrderType (см. OrderTypeFromTIF в client).
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
type TdMode string

const (
	// TdModeCross — cross-margin (USD-M SWAP по умолчанию).
	TdModeCross TdMode = "cross"
	// TdModeIsolated — isolated margin.
	TdModeIsolated TdMode = "isolated"
	// TdModeCash — для SPOT-профиля; в SWAP не используется.
	TdModeCash TdMode = "cash"
)

// InstType — тип инструмента OKX. SWAP-профиль всегда отдаёт InstTypeSWAP.
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

// PositionMode — режим позиций аккаунта (см. SetPositionMode).
type PositionMode string

const (
	// PositionModeNet — net mode (одна позиция на инструмент). Default для SDK.
	PositionModeNet PositionMode = "net_mode"
	// PositionModeLongShort — hedge mode (long + short одновременно).
	PositionModeLongShort PositionMode = "long_short_mode"
)

// OrderState — минимальный enum статуса ордера. Введён как компромисс между
// ТЗ §5.4 («не вводить общую модель статусов») и реальной потребностью деска
// фильтровать live/filled/canceled (см. ответы на ТЗ).
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
