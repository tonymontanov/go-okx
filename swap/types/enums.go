/*
ФАЙЛ: swap/types/enums.go

ОПИСАНИЕ:
Enum'ы SWAP-профиля OKX. Большая часть значений общая с остальными
профилями (spot и т. д.) — реэкспортируется через алиасы из пакета
github.com/tonymontanov/go-okx/v2/types (см. types/enums.go).

Здесь живут ТОЛЬКО enum'ы и константы, специфичные для SWAP:
  - PosSide        — long/short/net (стороны позиции в hedge-режиме).
  - PositionMode   — net_mode/long_short_mode (режим аккаунта).
  - OrderTypeOptimalLimitIOC — swap-only константа над общим OrderType
                               (используется ClosePosition по рынку).

BACKWARDS COMPATIBILITY:
Все ранее доступные через swap/types типы продолжают работать без изменений
на стороне пользователя. type-alias `type X = types.X` на уровне Go
эквивалентен исходному типу — нет necessary приведений или импортов.
*/

package types

import (
	commontypes "github.com/tonymontanov/go-okx/v2/types"
)

// SideType — направление ордера. См. commontypes.SideType.
type SideType = commontypes.SideType

const (
	// SideTypeBuy — покупка.
	SideTypeBuy = commontypes.SideTypeBuy
	// SideTypeSell — продажа.
	SideTypeSell = commontypes.SideTypeSell
)

// PosSide — сторона позиции в SWAP. Используется в hedge-режиме (long_short_mode).
// В net_mode (по умолчанию для SDK) всегда передаётся "net".
//
// SWAP-ONLY: на cash spot позиций нет, в spot/types этого типа нет.
type PosSide string

const (
	// PosSideLong — длинная сторона (hedge mode).
	PosSideLong PosSide = "long"
	// PosSideShort — короткая сторона (hedge mode).
	PosSideShort PosSide = "short"
	// PosSideNet — net-режим (одна позиция на инструмент).
	PosSideNet PosSide = "net"
)

// OrderType — тип ордера в нотации OKX. См. commontypes.OrderType.
type OrderType = commontypes.OrderType

const (
	// OrderTypeMarket — рыночный ордер.
	OrderTypeMarket = commontypes.OrderTypeMarket
	// OrderTypeLimit — лимитный (GTC).
	OrderTypeLimit = commontypes.OrderTypeLimit
	// OrderTypePostOnly — лимитный post-only (аналог TIF=GTX у Binance).
	OrderTypePostOnly = commontypes.OrderTypePostOnly
	// OrderTypeFOK — fill-or-kill.
	OrderTypeFOK = commontypes.OrderTypeFOK
	// OrderTypeIOC — immediate-or-cancel.
	OrderTypeIOC = commontypes.OrderTypeIOC
	// OrderTypeOptimalLimitIOC — SWAP-only market-ордер (исполняется по best
	// price + IOC, используется ClosePosition по рынку). На SPOT OKX вернёт
	// ошибку, поэтому константа живёт здесь, а не в общем пакете.
	OrderTypeOptimalLimitIOC OrderType = "optimal_limit_ioc"
)

// TimeInForceType — TIF в нотации core/types (Binance-style). См. commontypes.
type TimeInForceType = commontypes.TimeInForceType

const (
	// TimeInForceTypeGTC — Good Till Cancel (лимит).
	TimeInForceTypeGTC = commontypes.TimeInForceTypeGTC
	// TimeInForceTypeIOC — Immediate or Cancel.
	TimeInForceTypeIOC = commontypes.TimeInForceTypeIOC
	// TimeInForceTypeFOK — Fill or Kill.
	TimeInForceTypeFOK = commontypes.TimeInForceTypeFOK
	// TimeInForceTypeGTX — Post Only.
	TimeInForceTypeGTX = commontypes.TimeInForceTypeGTX
)

// TdMode — margin-mode ордера/позиции в OKX. См. commontypes.TdMode.
type TdMode = commontypes.TdMode

const (
	// TdModeCross — cross-margin (USD-M SWAP по умолчанию).
	TdModeCross = commontypes.TdModeCross
	// TdModeIsolated — isolated margin.
	TdModeIsolated = commontypes.TdModeIsolated
	// TdModeCash — для SPOT-профиля; в SWAP не используется, но константа
	// доступна для случаев, когда swap-код передаёт значение, полученное
	// из общего слоя.
	TdModeCash = commontypes.TdModeCash
)

// InstType — тип инструмента OKX. SWAP-профиль всегда отдаёт InstTypeSWAP.
// См. commontypes.InstType.
type InstType = commontypes.InstType

const (
	// InstTypeSpot — спотовый инструмент.
	InstTypeSpot = commontypes.InstTypeSpot
	// InstTypeSWAP — USD-M Perpetual SWAP.
	InstTypeSWAP = commontypes.InstTypeSWAP
	// InstTypeFutures — фьючерс с экспирацией.
	InstTypeFutures = commontypes.InstTypeFutures
	// InstTypeOption — опцион.
	InstTypeOption = commontypes.InstTypeOption
)

// PositionMode — режим позиций аккаунта (см. SetPositionMode).
//
// SWAP-ONLY: cash spot не имеет понятия позиции, режим там не настраивается.
type PositionMode string

const (
	// PositionModeNet — net mode (одна позиция на инструмент). Default для SDK.
	PositionModeNet PositionMode = "net_mode"
	// PositionModeLongShort — hedge mode (long + short одновременно).
	PositionModeLongShort PositionMode = "long_short_mode"
)

// OrderState — минимальный enum статуса ордера. См. commontypes.OrderState.
type OrderState = commontypes.OrderState

const (
	// OrderStateLive — активен, ожидает исполнения.
	OrderStateLive = commontypes.OrderStateLive
	// OrderStatePartiallyFilled — частично исполнен, остаток активен.
	OrderStatePartiallyFilled = commontypes.OrderStatePartiallyFilled
	// OrderStateFilled — полностью исполнен.
	OrderStateFilled = commontypes.OrderStateFilled
	// OrderStateCanceled — отменён.
	OrderStateCanceled = commontypes.OrderStateCanceled
	// OrderStateUnknown — не удалось распарсить статус (защитный fallback).
	OrderStateUnknown = commontypes.OrderStateUnknown
)

// ParseOrderState — реэкспорт функции из общего пакета. Вызывать как
// swaptypes.ParseOrderState(s) — поведение не изменилось.
var ParseOrderState = commontypes.ParseOrderState

// CancelAllAfterResult — ответ POST /api/v5/trade/cancel-all-after.
// См. commontypes.CancelAllAfterResult и types/cancel-all-after.go.
type CancelAllAfterResult = commontypes.CancelAllAfterResult
